//go:build linux

package nodo

import (
	"bytes"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// watchFile watches the directory, not the file. An editor or updater that
// replaces config.json by renaming a new file over it would leave a watch on
// the old inode listening to nothing; a directory watch filtered by name sees
// in-place writes and replacements alike.
func watchFile(path string) (<-chan struct{}, func(), error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, nil, err
	}
	const mask = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_CREATE | unix.IN_DELETE
	if _, err := unix.InotifyAddWatch(fd, filepath.Dir(path), mask); err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	// Wrapping the non-blocking descriptor in an os.File puts it on Go's
	// poller, which is what lets Close unblock a pending Read.
	f := os.NewFile(uintptr(fd), "inotify")
	name := []byte(filepath.Base(path))
	events := make(chan struct{}, 1)

	go func() {
		defer close(events)
		buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
		for {
			n, err := f.Read(buf)
			if err != nil {
				return
			}
			for off := 0; off+unix.SizeofInotifyEvent <= n; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				nameBytes := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
				off += unix.SizeofInotifyEvent + int(ev.Len)
				if bytes.Equal(bytes.TrimRight(nameBytes, "\x00"), name) {
					select {
					case events <- struct{}{}:
					default: // one pending event is as good as many
					}
				}
			}
		}
	}()
	return events, func() { f.Close() }, nil
}
