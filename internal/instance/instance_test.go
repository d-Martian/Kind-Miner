//go:build linux

package instance

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// Uses the real session bus when there is one; CI without a session skips.
func TestSecondLaunchActivatesTheFirst(t *testing.T) {
	if conn, err := dbus.ConnectSessionBus(); err != nil {
		t.Skip("no session bus")
	} else {
		conn.Close()
	}
	id := fmt.Sprintf("io.github.kind_miner.Test%d", os.Getpid())

	activated := make(chan struct{}, 1)
	status, release := Claim(id, func() { activated <- struct{}{} })
	if status != Held {
		t.Fatal("the first launch was not the instance")
	}
	defer release()

	second, _ := Claim(id, func() { t.Error("the second launch was activated") })
	if second != Running {
		t.Fatal("a second launch also became the instance")
	}
	select {
	case <-activated:
	case <-time.After(2 * time.Second):
		t.Fatal("the running instance was never asked to show itself")
	}

	// Once the first lets go, a new launch becomes the instance again.
	release()
	third, release3 := Claim(id, nil)
	if third != Held {
		t.Error("after release, a new launch could not become the instance")
	}
	release3()
}
