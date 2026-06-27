package afmpeg_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// TestRunCommand runs a built command end-to-end over a MemMapFs (composing
// 0003/0004), proving the builder → runtime path.
func TestRunCommand(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.NewCommand(afmpeg.WithInput("in"), afmpeg.WithOutput("out"))

	res, err := newTestRuntime(t).RunCommand(context.Background(), afero.NewMemMapFs(), cmd)
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
}

// TestRunCommand_PassesArgs proves RunCommand forwards the rendered Args() to the
// guest: a global-raw "exit:4" lands as argv[0] and the guest exits 4.
func TestRunCommand_PassesArgs(t *testing.T) {
	t.Parallel()

	cmd := afmpeg.Command{Global: afmpeg.Global{Raw: []string{"exit:4"}}}

	res, err := newTestRuntime(t).RunCommand(context.Background(), afero.NewMemMapFs(), cmd)
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if res.ExitCode != 4 {
		t.Fatalf("ExitCode = %d, want 4 (args not forwarded?)", res.ExitCode)
	}
}
