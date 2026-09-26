//go:build linux

package nodo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchFileSeesInPlaceWritesAndReplacements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(stockConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	events, closeWatch, err := watchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWatch()

	expect := func(what string) {
		t.Helper()
		select {
		case <-events:
		case <-time.After(2 * time.Second):
			t.Fatalf("no event after %s", what)
		}
	}
	// Nodo's SSH UI: truncate and rewrite in place.
	if err := os.WriteFile(path, []byte(stockConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("an in-place rewrite")

	// An updater that writes a new file and renames it over the old one.
	tmp := filepath.Join(dir, "config.json.new")
	if err := os.WriteFile(tmp, []byte(stockConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	for len(events) > 0 { // the temp file's own events are filtered, but drain to be sure
		<-events
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	expect("a rename over the file")
}

func TestWatchFileIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	events, closeWatch, err := watchFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeWatch()
	// Nodo's touchscreen UI takes config.json.lock around its writes.
	if err := os.WriteFile(filepath.Join(dir, "config.json.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
		t.Error("an event for the lock file was reported as a config change")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWatchFileEndsOnClose(t *testing.T) {
	events, closeWatch, err := watchFile(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	closeWatch()
	select {
	case _, ok := <-events:
		if ok {
			t.Error("got an event instead of the channel closing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("closing the watch did not end it; Shutdown would leak the goroutine")
	}
}
