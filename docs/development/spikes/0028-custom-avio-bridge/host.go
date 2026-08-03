//go:build ignore

// Go host: an in-memory file store (afero stand-in) bridged to the native libav
// spike over a Unix socket. Proves all I/O — incl. the non-frag-MP4 moov-patch
// backward seeks — round-trips through in-memory storage, no host file for in/out.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type memFile struct{ data []byte }

var (
	mu                              sync.Mutex
	files                           = map[string]*memFile{}
	reads, writes, seeks, backSeeks int64
)

func rd(c net.Conn, n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		return nil
	}
	return b
}
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func u64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

func serve(c net.Conn) {
	defer c.Close()
	hdr := rd(c, 6)
	if hdr == nil || hdr[0] != 'O' {
		return
	}
	mode := hdr[1]
	name := string(rd(c, int(u32(hdr[2:]))))
	mu.Lock()
	f := files[name]
	if f == nil && mode == 'w' {
		f = &memFile{}
		files[name] = f
	}
	mu.Unlock()
	if f == nil {
		c.Write([]byte{1})
		return
	}
	if mode == 'w' {
		f.data = f.data[:0]
	}
	c.Write([]byte{0})
	var off, maxW int64
	for {
		op := rd(c, 1)
		if op == nil {
			return
		}
		switch op[0] {
		case 'R':
			n := int(u32(rd(c, 4)))
			atomic.AddInt64(&reads, 1)
			var chunk []byte
			if off < int64(len(f.data)) {
				end := off + int64(n)
				if end > int64(len(f.data)) {
					end = int64(len(f.data))
				}
				chunk = f.data[off:end]
			}
			binary.Write(c, binary.LittleEndian, uint32(len(chunk)))
			c.Write(chunk)
			off += int64(len(chunk))
		case 'W':
			n := int(u32(rd(c, 4)))
			buf := rd(c, n)
			atomic.AddInt64(&writes, 1)
			need := off + int64(n)
			if need > int64(len(f.data)) {
				f.data = append(f.data, make([]byte, need-int64(len(f.data)))...)
			}
			copy(f.data[off:], buf)
			off += int64(n)
			if off > maxW {
				maxW = off
			}
			binary.Write(c, binary.LittleEndian, uint32(n))
		case 'S':
			target := int64(u64(rd(c, 8)))
			whence := rd(c, 1)[0]
			atomic.AddInt64(&seeks, 1)
			switch whence {
			case 0:
				off = target
			case 1:
				off += target
			case 2:
				off = int64(len(f.data)) + target
			}
			if mode == 'w' && off < maxW {
				atomic.AddInt64(&backSeeks, 1)
			}
			binary.Write(c, binary.LittleEndian, uint64(off))
		case 'Z':
			binary.Write(c, binary.LittleEndian, uint64(len(f.data)))
		case 'C':
			return
		}
	}
}

func boxes(tag string, d []byte) {
	find := func(s string) int {
		for i := 0; i+4 <= len(d); i++ {
			if string(d[i:i+4]) == s {
				return i
			}
		}
		return -1
	}
	ftyp, mdat, moov, moof := find("ftyp"), find("mdat"), find("moov"), find("moof")
	kind := "??"
	if moof >= 0 {
		kind = "FRAGMENTED (moof present)"
	} else if moov > mdat && mdat >= 0 {
		kind = "non-fragmented (moov after mdat)"
	} else if moov >= 0 {
		kind = "non-fragmented (moov before mdat)"
	}
	fmt.Printf("  %-8s %d bytes | ftyp@%d mdat@%d moov@%d moof@%d -> %s\n", tag, len(d), ftyp, mdat, moov, moof, kind)
}

func main() {
	sock := "/tmp/spike.sock"
	os.Remove(sock)
	exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=15", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "3", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "/tmp/in.mp4").Run()
	indata, _ := os.ReadFile("/tmp/in.mp4")
	files["in.mp4"] = &memFile{data: indata}
	fmt.Printf("fixture in.mp4: %d bytes (loaded into in-memory store)\n", len(indata))
	boxes("in.mp4", indata)

	ln, err := net.Listen("unix", sock)
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go serve(c)
		}
	}()

	cmd := exec.Command("./avio_spike", sock, "in.mp4", "out.mp4")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	fmt.Printf("avio_spike exit: %v\n", runErr)

	mu.Lock()
	out := files["out.mp4"]
	mu.Unlock()
	if out == nil || len(out.data) == 0 {
		fmt.Println("FAIL: no output produced in the store")
		os.Exit(1)
	}
	os.WriteFile("/tmp/out.mp4", out.data, 0o644)
	fmt.Printf("\n=== result ===\n  output landed ENTIRELY in the in-memory store: %d bytes\n", len(out.data))
	boxes("out.mp4", out.data)
	probe, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=format_name,duration,nb_streams", "-of", "default=nw=1", "/tmp/out.mp4").CombinedOutput()
	fmt.Printf("  ffprobe(out.mp4): %s", probe)
	fmt.Printf("  IPC calls: reads=%d writes=%d seeks=%d  BACKWARD-seeks-on-output(moov patch)=%d\n", reads, writes, seeks, backSeeks)
	if backSeeks > 0 {
		fmt.Println("  ✅ seekable custom-AVIO wrote a NON-FRAGMENTED MP4 via backward seeks — the case HTTP PUT cannot do")
	}
}
