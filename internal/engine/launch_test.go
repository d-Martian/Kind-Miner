//go:build unix

package engine

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLaunch(t *testing.T) {
	// Resolved, because sh prints the physical directory.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		script string
		adjust func(*exec.Cmd)
		want   []string // lines of output, in any order
	}{
		{
			// Every engine's readOutput watches one stream; an error p2pool
			// prints to stderr is how it says why it would not start.
			name:   "stdout and stderr arrive on one reader",
			script: "echo out; echo err >&2",
			want:   []string{"out", "err"},
		},
		{
			name:   "the engine's adjustment applies",
			script: "pwd",
			adjust: func(c *exec.Cmd) { c.Dir = dir },
			want:   []string{dir},
		},
		{
			name:   "the engine sees our environment",
			script: `echo "$KM_LAUNCH_TEST"`,
			want:   []string{"inherited"},
		},
	}
	t.Setenv("KM_LAUNCH_TEST", "inherited")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, out, cancel, err := launch("sh", "sh", []string{"-c", c.script}, c.adjust)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			got, err := io.ReadAll(out)
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			lines := strings.Fields(string(got))
			for _, w := range c.want {
				if !slices.Contains(lines, w) {
					t.Errorf("output %q is missing %q", got, w)
				}
			}
		})
	}
}

func TestLaunchFailureNamesTheEngine(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "p2pool")
	_, _, _, err := launch("p2pool", missing, nil, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "starting p2pool: ") {
		t.Fatalf("err = %v, want one naming p2pool", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want the cause kept", err)
	}
}
