package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestIsTerminalFD(t *testing.T) {
	// /dev/null is what a launcher gives an app as stdin. It is a character
	// device, and taking it for a terminal sent every menu launch to tray mode.
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if isTerminalFD(int(null.Fd())) {
		t.Error("/dev/null was taken for a terminal")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminalFD(int(r.Fd())) {
		t.Error("a pipe was taken for a terminal")
	}

	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminalFD(int(f.Fd())) {
		t.Error("a file was taken for a terminal")
	}

	// And a real one: the far end of a pseudo-terminal, as a shell has.
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	defer ptmx.Close()
	if err := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("cannot unlock a pseudo-terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("cannot name the pseudo-terminal: %v", err)
	}
	pts, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo-terminal: %v", err)
	}
	defer pts.Close()
	if !isTerminalFD(int(pts.Fd())) {
		t.Error("a pseudo-terminal was not taken for a terminal")
	}
}
