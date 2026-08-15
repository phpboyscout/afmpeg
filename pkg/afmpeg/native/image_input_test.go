package native_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg/native"
)

// TestIntegration_ImageInput_BackendParity is the afmpeg#6 regression test: a
// still image as a process input must behave the same on both backends.
//
// It is a differential loop — one Command, two backends, WASM as the control.
// The bug it pins is *not* about looping, despite how it was first reported: the
// native bridge opens inputs with a NULL filename and a custom AVIO, so demuxer
// selection is content-probe only, and the lean engine builds `image2` (an
// AVFMT_NOFILE demuxer that ignores custom I/O) without the stream-based
// `image_png_pipe` that content probing would otherwise pick. The result is that
// no still image can be opened on Backend B at all.
//
// Fixed in ffmpeg-wasi n8.1.2-12, which builds image_png_pipe; this is the
// consumer-side proof, and it goes red again if a future engine drops it. Both
// artifacts need only the lean profile.
func TestIntegration_ImageInput_BackendParity(t *testing.T) {
	t.Parallel()

	module := integrationModule(t)
	driver := integrationDriver(t, afmpeg.ProfileLean, false)

	backends := map[string]afmpeg.Option{
		"wasm":   afmpeg.WithModuleFile(module),
		"native": afmpeg.WithBackend(native.New(native.WithNativeBinary(driver))),
	}

	for name, opt := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			rt, err := afmpeg.New(ctx, opt)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			t.Cleanup(func() { _ = rt.Close(ctx) })

			fs := afero.NewMemMapFs()
			if err := afero.WriteFile(fs, "still.png", flatPNG(t), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			res, err := rt.RunJob(ctx, fs, afmpeg.Command{
				Inputs:        []afmpeg.Input{{Path: "still.png"}},
				FilterComplex: "[0:v]format=yuv420p[vout]",
				Outputs: []afmpeg.Output{{
					Path:       "out.mp4",
					Map:        []string{"[vout]"},
					VideoCodec: "libopenh264",
				}},
			})
			if err != nil {
				t.Fatalf("RunJob: %v", err)
			}

			if res.ExitCode != 0 {
				t.Fatalf("engine exit %d on a still-image input: %s", res.ExitCode, res.Stderr)
			}

			out, err := afero.ReadFile(fs, "out.mp4")
			if err != nil {
				t.Fatalf("read output: %v", err)
			}

			if len(out) == 0 {
				t.Fatal("wrote an empty output")
			}

			t.Logf("%s wrote %d bytes", name, len(out))
		})
	}
}

// flatPNG builds a small flat-colour PNG in memory.
func flatPNG(t *testing.T) []byte {
	t.Helper()

	const w, h = 320, 240

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0xC0, G: 0x20, B: 0x20, A: 0xFF})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	return buf.Bytes()
}
