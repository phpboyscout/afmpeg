package afmpeg_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

func TestSandbox_Confined(t *testing.T) {
	t.Parallel()

	cases := map[afmpeg.Sandbox]bool{
		afmpeg.SandboxWASI:        true,
		afmpeg.SandboxLandlock:    true,
		afmpeg.SandboxUnavailable: false,
		afmpeg.SandboxDisabled:    false,
		"":                        false,
		"something-new":           false,
	}
	for s, want := range cases {
		if got := s.Confined(); got != want {
			t.Errorf("Sandbox(%q).Confined() = %v, want %v", s, got, want)
		}
	}
}

// The wasm engine's own field says "unavailable" because its Landlock code is
// compiled out, so passing it through would report every wasm job as exposed.
func TestRun_WASMReportsItsSandbox(t *testing.T) {
	t.Parallel()

	res, err := newTestRuntime(t).Run(context.Background(), afero.NewMemMapFs(), "exit:0")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Sandbox != afmpeg.SandboxWASI {
		t.Fatalf("Sandbox = %q, want %q", res.Sandbox, afmpeg.SandboxWASI)
	}
}
