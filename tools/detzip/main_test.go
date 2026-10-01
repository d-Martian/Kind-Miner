package main

import (
	"archive/zip"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"pkg/kind-miner.exe":      "binary",
		"pkg/README.md":           "readme",
		"pkg/config.example.yaml": "wallet: ''",
		"pkg/sub/z.txt":           "z",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sum(t *testing.T, p string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func TestSameBytesWhereverAndWhenever(t *testing.T) {
	a, b := tree(t), tree(t)
	// Different file times and a different local zone must change nothing.
	os.Chtimes(filepath.Join(b, "pkg", "README.md"), time.Now(), time.Now().Add(-time.Hour))
	outA, outB := filepath.Join(t.TempDir(), "a.zip"), filepath.Join(t.TempDir(), "b.zip")
	if err := run(outA, a, "pkg", "1790000000"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TZ", "Pacific/Kiritimati")
	time.Local = time.FixedZone("far", 14*3600)
	defer func() { time.Local = time.UTC }()
	if err := run(outB, b, "pkg", "1790000000"); err != nil {
		t.Fatal(err)
	}
	if sum(t, outA) != sum(t, outB) {
		t.Fatal("the same files zipped twice came out different")
	}

	r, err := zip.OpenReader(outA)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
		if !f.Modified.Equal(time.Unix(1790000000, 0)) {
			t.Errorf("%s: time %s", f.Name, f.Modified)
		}
	}
	want := []string{"pkg/README.md", "pkg/config.example.yaml", "pkg/kind-miner.exe", "pkg/sub/z.txt"}
	if len(names) != len(want) {
		t.Fatalf("members %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("member %d = %s, want %s (sorted, no directories)", i, names[i], want[i])
		}
	}
	if m := r.File[2].Mode().Perm(); m != 0o755 {
		t.Errorf("the .exe is %v, want 0755", m)
	}
}

func TestNeedsAnEpoch(t *testing.T) {
	if err := run(filepath.Join(t.TempDir(), "x.zip"), tree(t), "pkg", ""); err == nil {
		t.Error("zipped without SOURCE_DATE_EPOCH")
	}
}
