package gui

import (
	"errors"
	"testing"

	"github.com/kind-miner/kind-miner/internal/core"
)

type fakeBringUp struct {
	err       error
	shutdowns int
}

func (f *fakeBringUp) Start(func(core.Step)) error { return f.err }
func (f *fakeBringUp) Shutdown()                   { f.shutdowns++ }

func TestStartupTearsDownAFailedStart(t *testing.T) {
	for _, tc := range []struct {
		name          string
		err           error
		wantShutdowns int
	}{
		// What Retry depends on: a p2pool still syncing when Start gave up
		// must not survive into the next attempt and hold its ports.
		{"a failed start is torn down before Retry can start again",
			errors.New("p2pool did not become ready within 3m0s"), 1},
		{"a successful start keeps running", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sup := &fakeBringUp{err: tc.err}
			if err := startOrTearDown(sup, nil); err != tc.err {
				t.Errorf("startOrTearDown = %v; want Start's own error %v", err, tc.err)
			}
			if sup.shutdowns != tc.wantShutdowns {
				t.Errorf("Shutdown called %d times; want %d", sup.shutdowns, tc.wantShutdowns)
			}
		})
	}
}
