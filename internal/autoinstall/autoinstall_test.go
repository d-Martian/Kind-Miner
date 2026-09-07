package autoinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	// Nest under a directory so base-name matching is exercised, mirroring how
	// upstream archives wrap the binary in a versioned folder.
	hdr := &tar.Header{Name: "pkg/" + name, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("pkg/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestVerifyAndExtractTarGz(t *testing.T) {
	content := []byte("fake xmrig binary")
	archive := makeTarGz(t, "xmrig", content)
	got, err := verifyAndExtract(archive, "xmrig", sha(archive), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q, want %q", got, content)
	}
}

func TestVerifyAndExtractZip(t *testing.T) {
	content := []byte("fake xmrig.exe")
	archive := makeZip(t, "xmrig.exe", content)
	got, err := verifyAndExtract(archive, "xmrig.exe", sha(archive), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q, want %q", got, content)
	}
}

func TestVerifyAndExtractHashMismatch(t *testing.T) {
	archive := makeTarGz(t, "xmrig", []byte("payload"))
	// Flip the first byte of the expected hash so it no longer matches.
	wrong := "00" + sha(archive)[2:]
	if _, err := verifyAndExtract(archive, "xmrig", wrong, false); err == nil {
		t.Fatal("expected verification error for wrong hash, got nil")
	}
}

func TestVerifyAndExtractMissingMember(t *testing.T) {
	archive := makeTarGz(t, "somethingelse", []byte("payload"))
	if _, err := verifyAndExtract(archive, "xmrig", sha(archive), false); err == nil {
		t.Fatal("expected error when the wanted binary is absent from the archive")
	}
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestDepsManifestPinned guards the embedded deps.json against regressions:
// every pinned asset must carry a real 64-char hex SHA256 (no placeholders) and
// a filename that embeds the pinned version.
func TestDepsManifestPinned(t *testing.T) {
	for name, spec := range map[string]depSpec{"xmrig": deps.XMRig, "p2pool": deps.P2Pool} {
		if spec.Version == "" {
			t.Errorf("%s: empty version", name)
		}
		if spec.Repo == "" {
			t.Errorf("%s: empty repo", name)
		}
		if !strings.Contains(spec.URLTemplate, "{version}") || !strings.Contains(spec.URLTemplate, "{file}") {
			t.Errorf("%s: url_template missing placeholders: %q", name, spec.URLTemplate)
		}
		if len(spec.Assets) == 0 {
			t.Errorf("%s: no assets pinned", name)
		}
		for plat, a := range spec.Assets {
			if !sha256Re.MatchString(a.SHA256) {
				t.Errorf("%s/%s: sha256 not 64-char hex (placeholder?): %q", name, plat, a.SHA256)
			}
			if !strings.Contains(a.File, spec.Version) {
				t.Errorf("%s/%s: file %q does not contain version %q", name, plat, a.File, spec.Version)
			}
		}
	}
}

// TestLinuxAmd64Pinned ensures the primary distribution target is never dropped.
func TestLinuxAmd64Pinned(t *testing.T) {
	for name, spec := range map[string]depSpec{"xmrig": deps.XMRig, "p2pool": deps.P2Pool} {
		if _, ok := spec.Assets["linux-amd64"]; !ok {
			t.Errorf("%s: missing linux-amd64 pin", name)
		}
	}
}

func TestAssetURL(t *testing.T) {
	spec := depSpec{Version: "1.2.3", URLTemplate: "https://example/v{version}/{file}"}
	a := depAsset{File: "tool-1.2.3.tar.gz"}
	want := "https://example/v1.2.3/tool-1.2.3.tar.gz"
	if got := assetURL(spec, a); got != want {
		t.Fatalf("assetURL = %q, want %q", got, want)
	}
}

// install writes a fake binary and its stamp, standing in for a completed
// ensurePinned run.
func install(t *testing.T, dir, name, version string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeStamp(path, pinStamp{Version: version, SHA256: sha256Hex(content)}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallReason(t *testing.T) {
	body := []byte("a pinned binary")

	t.Run("the pinned build, untouched, is left alone", func(t *testing.T) {
		p := install(t, t.TempDir(), "xmrig", "6.26.0", body)
		if got := installReason(p, "6.26.0"); got != "" {
			t.Errorf("installReason = %q, want no reason to reinstall", got)
		}
	})

	t.Run("a moved pin is fetched, which is the bug this fixes", func(t *testing.T) {
		// Before the stamp existed, ensurePinned returned early on any file that
		// was present, so a deps.json bump never reached an existing install.
		p := install(t, t.TempDir(), "xmrig", "6.21.3", body)
		if got := installReason(p, "6.26.0"); got == "" {
			t.Error("a version bump must trigger a reinstall, got none")
		}
	})

	t.Run("a modified binary is fetched again", func(t *testing.T) {
		dir := t.TempDir()
		p := install(t, dir, "xmrig", "6.26.0", body)
		if err := os.WriteFile(p, []byte("something else entirely"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := installReason(p, "6.26.0"); got == "" {
			t.Error("a binary that no longer matches its stamp must be replaced")
		}
	})

	t.Run("an install predating the stamp is fetched again", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "xmrig")
		if err := os.WriteFile(p, body, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := installReason(p, "6.26.0"); got == "" {
			t.Error("an unstamped binary cannot be vouched for and must be replaced")
		}
	})

	t.Run("a missing binary is installed", func(t *testing.T) {
		if got := installReason(filepath.Join(t.TempDir(), "xmrig"), "6.26.0"); got == "" {
			t.Error("a missing binary must be installed")
		}
	})
}
