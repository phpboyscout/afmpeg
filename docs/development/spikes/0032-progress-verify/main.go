//go:build spike

// Spike 0032 verify — prove the v9 engine emits progress records.
//
// afmpeg's vfs does not yet serve /dev/afmpeg-progress, so the engine's writes
// land in the MemMapFs as a regular file. We run a progress:true process job
// against the new engine and read that file back — if it holds NDJSON
// {frame,out_time_us,total_size} records, engine emission works.
//
//	AFMPEG_TEST_FFMPEG_WASI=/path/to/new/ffmpeg-wasi-lgpl.wasm \
//	  go run -tags spike ./docs/development/spikes/0032-progress-verify
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

func main() {
	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		fmt.Fprintln(os.Stderr, "set AFMPEG_TEST_FFMPEG_WASI to the newly-built v9 .wasm")
		os.Exit(2)
	}

	ctx := context.Background()
	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		fmt.Fprintln(os.Stderr, "New:", err)
		os.Exit(1)
	}
	defer func() { _ = rt.Close(ctx) }()

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "in.wav", makeWAV(44100, 12), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}

	// Raw spec with the v9 progress flag. (afmpeg's typed API doesn't set it yet
	// — that's the next task; here we drive the engine directly.)
	spec := `{"op":"process","version":9,"progress":true,` +
		`"inputs":[{"path":"in.wav"}],"outputs":[{"path":"out.mp4","audio_codec":"aac"}]}`

	res, err := rt.Run(ctx, fs, spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Run:", err)
		os.Exit(1)
	}
	fmt.Printf("engine exit=%d\n", res.ExitCode)
	if res.ExitCode != 0 {
		fmt.Fprintln(os.Stderr, "stderr:\n", res.Stderr)
		os.Exit(1)
	}

	// The engine wrote its progress records here (vfs routed the unknown device
	// path to the backing fs).
	for _, p := range []string{"dev/afmpeg-progress", "/dev/afmpeg-progress"} {
		b, err := afero.ReadFile(fs, p)
		if err == nil && len(b) > 0 {
			fmt.Printf("\n=== %s (%d bytes) ===\n%s", p, len(b), b)
			fmt.Println("\nVERDICT: the v9 engine emits progress records ✓")
			return
		}
	}
	fmt.Fprintln(os.Stderr, "\nNO progress file found — engine did not emit")
	os.Exit(1)
}

func makeWAV(rate, seconds int) []byte {
	n := rate * seconds
	dataLen := n * 2
	buf := make([]byte, 44+dataLen)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))
	for i := 0; i < n; i++ {
		s := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(s))
	}
	return buf
}
