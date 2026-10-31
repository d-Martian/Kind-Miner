package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// launch starts bin as an engine subprocess, the one way every engine is
// started: its stdout and stderr on one pipe for the engine's readOutput, and
// dieWithUs set so it cannot outlive kind-miner. adjust, when not nil, sets
// whatever else this engine needs — Tor's environment, p2pool's working
// directory — before the process starts. what names the engine in the error.
//
// On failure nothing is left running, so the caller has nothing to clean up.
func launch(what, bin string, args []string, adjust func(*exec.Cmd)) (*exec.Cmd, io.Reader, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	// Explicit rather than nil: with a nil Env and a Dir, os/exec rewrites PWD
	// to match, and the engines have always seen ours.
	cmd.Env = os.Environ()
	if adjust != nil {
		adjust(cmd)
	}
	// After adjust, so nothing it sets can drop the parent-death signal. It
	// survives systemd-run's exec into the miner: only a set-user-ID or
	// capability-carrying binary clears it, and systemd-run is neither.
	dieWithUs(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("starting %s: %w", what, err)
	}
	return cmd, stdout, cancel, nil
}
