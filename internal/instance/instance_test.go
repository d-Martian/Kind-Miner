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
	id := fmt.Sprintf("io.github.d_Martian.Test%d", os.Getpid())

	activated := make(chan struct{}, 1)
	status, release := Claim(id, nil, func() { activated <- struct{}{} })
	if status != Held {
		t.Fatal("the first launch was not the instance")
	}
	defer release()

	second, _ := Claim(id, nil, func() { t.Error("the second launch was activated") })
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
	third, release3 := Claim(id, nil, nil)
	if third != Held {
		t.Error("after release, a new launch could not become the instance")
	}
	release3()
}

// A rename is when two versions meet: the new Flatpak installs beside the old
// one, and an older kind-minerd still claims the old name. Each must see the
// other, or both mine at once.
func TestARenamedVersionAndAnOldOneNeverBothRun(t *testing.T) {
	if conn, err := dbus.ConnectSessionBus(); err != nil {
		t.Skip("no session bus")
	} else {
		conn.Close()
	}
	current := fmt.Sprintf("io.github.d_Martian.Renamed%d", os.Getpid())
	former := fmt.Sprintf("io.github.kind_miner.Renamed%d", os.Getpid())

	t.Run("an old version running keeps the new one from starting", func(t *testing.T) {
		activated := make(chan struct{}, 1)
		old, releaseOld := Claim(former, nil, func() { activated <- struct{}{} })
		if old != Held {
			t.Fatal("the old version did not become the instance")
		}
		defer releaseOld()

		if got, _ := Claim(current, []string{former}, nil); got != Running {
			t.Fatalf("the new version started beside the old one (%v)", got)
		}
		select {
		case <-activated:
		case <-time.After(2 * time.Second):
			t.Fatal("the old version was never asked to show itself")
		}
		// It gave the new name back on the way out, so it is free once the
		// old version is gone.
		releaseOld()
		got, release := Claim(current, []string{former}, nil)
		if got != Held {
			t.Fatalf("with the old version gone, the new one could not start (%v)", got)
		}
		release()
	})

	t.Run("the new version running keeps an old one from starting", func(t *testing.T) {
		activated := make(chan struct{}, 1)
		got, release := Claim(current, []string{former}, func() { activated <- struct{}{} })
		if got != Held {
			t.Fatalf("the new version did not become the instance (%v)", got)
		}
		defer release()

		if old, _ := Claim(former, nil, nil); old != Running {
			t.Fatalf("an old version started beside the new one (%v)", old)
		}
		select {
		case <-activated:
		case <-time.After(2 * time.Second):
			t.Fatal("the new version was not shown when the old one was launched")
		}
	})
}
