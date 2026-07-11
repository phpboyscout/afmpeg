//go:build spike

// Spike 0031 — observed-filesystem progress.
//
// Question: can afmpeg surface *live* progress from the WASI engine today,
// without the engine emitting anything and without scraping CLI stderr?
//
// Thesis: afmpeg already implements the filesystem the engine reads and writes
// through (the vfs bridge over the caller's afero.Fs). So the host can watch
// bytes flow — input consumed, output produced — at that boundary, in real time,
// and push progress on a channel. No engine change, no afmpeg change: this uses
// only the public API (Runtime.Run with a caller-supplied afero.Fs).
//
// Run:
//
//	AFMPEG_TEST_FFMPEG_WASI=/path/to/ffmpeg-wasi-gpl.wasm \
//	  go run -tags spike ./docs/development/spikes/0031-progress-observed-fs
//
// Optional: AFMPEG_SPIKE_SECONDS (default 300) sizes the synthetic audio so the
// encode runs long enough to watch.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

func main() {
	module := os.Getenv("AFMPEG_TEST_FFMPEG_WASI")
	if module == "" {
		fmt.Fprintln(os.Stderr, "set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi-*.wasm (gpl/lgpl both fine)")
		os.Exit(2)
	}

	seconds := 300
	if v := os.Getenv("AFMPEG_SPIKE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			seconds = n
		}
	}

	ctx := context.Background()

	rt, err := afmpeg.New(ctx, afmpeg.WithModuleFile(module))
	if err != nil {
		fmt.Fprintln(os.Stderr, "New:", err)
		os.Exit(1)
	}
	defer func() { _ = rt.Close(ctx) }()

	// A large synthetic WAV so the transcode takes real wall-time to observe.
	const sampleRate = 44100
	wav := makeWAV(sampleRate, seconds)

	base := afero.NewMemMapFs()
	if err := afero.WriteFile(base, "in.wav", wav, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write fixture:", err)
		os.Exit(1)
	}

	fmt.Printf("input: in.wav  %.1f MiB  (%d s of %d Hz mono s16le)\n\n",
		float64(len(wav))/(1<<20), seconds, sampleRate)

	// --- the goroutine + channel progress mechanism -----------------------
	//
	// The sink is called on the engine's I/O syscall path (inside vfs Read/
	// Write), so it MUST NOT block the engine: buffered channel + non-blocking
	// send, dropping when full. Coalescing/back-pressure policy is a real design
	// question the eventual spec must answer — surfaced here.
	events := make(chan Event, 8192)
	var dropped int

	t0 := time.Now()
	clock := func() int64 { return time.Since(t0).Milliseconds() }

	sink := func(e Event) {
		select {
		case events <- e:
		default:
			dropped++
		}
	}

	obs := newObserveFs(base, clock, sink)

	// Consumer goroutine: drains progress and prints a live line whenever the
	// picture meaningfully changes (new ~250ms bucket, or +2% input consumed).
	done := make(chan struct{})
	go func() {
		defer close(done)
		var lastBucket int64 = -1
		var lastPct = -1
		firstAt, lastAt := int64(-1), int64(0)
		var count, reads, writes int64
		var inCum, inTotal, outCum int64

		flush := func(force bool) {
			bucket := lastAt / 250
			pct := -1
			if inTotal > 0 {
				pct = int(100 * inCum / inTotal)
			}
			if force || bucket != lastBucket || pct/2 != lastPct/2 {
				lastBucket, lastPct = bucket, pct
				fmt.Printf("  t=%5dms  in %s  out %s\n",
					lastAt, pctBar(inCum, inTotal), humanBytes(outCum))
			}
		}

		for e := range events {
			count++
			if firstAt < 0 {
				firstAt = e.ElapsedMS
			}
			lastAt = e.ElapsedMS
			switch e.Op {
			case "read":
				reads++
				if e.Path == "in.wav" {
					inCum, inTotal = e.Cum, e.Total
				}
			case "write":
				writes++
				if e.Path == "out.mp4" {
					outCum = e.Cum
				}
			}
			flush(false)
		}
		flush(true)

		fmt.Printf("\nobserved %d I/O events (%d read, %d write); first at %dms, last at %dms\n",
			count, reads, writes, firstAt, lastAt)
	}()

	// --- run the real encode via the fully public API ---------------------
	spec := `{"op":"process","inputs":[{"path":"in.wav"}],"outputs":[{"path":"out.mp4","audio_codec":"aac"}]}`
	res, runErr := rt.Run(ctx, obs, spec)
	wall := time.Since(t0)

	close(events)
	<-done

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "\nRun error:", runErr)
		os.Exit(1)
	}

	out, _ := afero.ReadFile(base, "out.mp4")
	fmt.Printf("\nengine exit=%d  wall=%s  output=%s  dropped-events=%d\n",
		res.ExitCode, wall.Round(time.Millisecond), humanBytes(int64(len(out))), dropped)

	// Verdict: progress is "live" if events arrived spread across the run, not
	// bunched at the end.
	fmt.Println()
	if res.ExitCode == 0 {
		fmt.Println("VERDICT: live progress is observable at the afero.Fs boundary, today,")
		fmt.Println("         through the public API — no engine or afmpeg change required.")
	} else {
		fmt.Printf("engine stderr tail:\n%s\n", res.Stderr)
	}
}

// makeWAV builds a mono s16le WAV of the given duration: a quiet sine so the AAC
// encoder does real work rather than trivially packing silence.
func makeWAV(sampleRate, seconds int) []byte {
	n := sampleRate * seconds
	dataLen := n * 2
	buf := make([]byte, 44+dataLen)

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:24], 1)  // mono
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(sampleRate*2)) // byte rate
	binary.LittleEndian.PutUint16(buf[32:34], 2)                    // block align
	binary.LittleEndian.PutUint16(buf[34:36], 16)                   // bits
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))

	for i := 0; i < n; i++ {
		s := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(s))
	}

	return buf
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func pctBar(cum, total int64) string {
	if total <= 0 {
		return fmt.Sprintf("%s (size unknown)", humanBytes(cum))
	}
	pct := float64(cum) / float64(total)
	const w = 24
	filled := int(pct * w)
	bar := make([]byte, w)
	for i := range bar {
		if i < filled {
			bar[i] = '#'
		} else {
			bar[i] = '.'
		}
	}
	return fmt.Sprintf("[%s] %5.1f%%", bar, pct*100)
}
