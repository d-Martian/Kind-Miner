// Command detzip writes a reproducible zip: the same files and
// SOURCE_DATE_EPOCH give the same bytes, on any machine.
//
//	go run ./tools/detzip OUT.zip DIR MEMBER
//
// zips DIR/MEMBER (a file or a directory, recursively) with paths relative
// to DIR. It is scripts/package.sh's zip recipe, and replaces Info-ZIP's
// zip, which the Windows runner's Git Bash does not have and which stores
// modification times in the builder's local time zone — so the same files
// zipped in two time zones came out different.
//
// What it pins: member order (sorted, with "/" separators), every member's
// time (SOURCE_DATE_EPOCH, in UTC), permissions (0755 executables, 0644
// everything else), and no directory entries or extra fields.
package main

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: detzip OUT.zip DIR MEMBER")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3], os.Getenv("SOURCE_DATE_EPOCH")); err != nil {
		fmt.Fprintf(os.Stderr, "detzip: %v\n", err)
		os.Exit(1)
	}
}

func run(out, dir, member, epoch string) error {
	secs, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return fmt.Errorf("SOURCE_DATE_EPOCH must be set to a number of seconds, got %q", epoch)
	}
	mtime := time.Unix(secs, 0).UTC()

	var files []string
	root := filepath.Join(dir, member)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, name := range files {
		if err := add(zw, dir, name, mtime); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func add(zw *zip.Writer, dir, name string, mtime time.Time) error {
	src, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	defer src.Close()
	mode := fs.FileMode(0o644)
	if executable(name) {
		mode = 0o755
	}
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mtime}
	h.SetMode(mode)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

// executable is decided by name, not by the file's mode bits, which Windows
// does not keep.
func executable(name string) bool {
	return strings.HasSuffix(name, ".exe") || !strings.Contains(filepath.Base(name), ".")
}
