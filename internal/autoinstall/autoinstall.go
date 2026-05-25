// Package autoinstall downloads XMRig and p2pool on first run so users never
// have to manually install anything.
package autoinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BinDir returns the directory where bundled binaries are stored
// (next to the kind-miner executable).
func BinDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "bin"
	}
	return filepath.Join(filepath.Dir(exe), "bin")
}

// EnsureXMRig ensures the xmrig binary is present in binDir.
// If it is already there, this is a no-op. Otherwise it downloads the latest
// release from the official XMRig GitHub repository.
func EnsureXMRig(binDir string) (string, error) {
	return ensure(binDir, dep{
		name:  "xmrig",
		repo:  "xmrig/xmrig",
		asset: xmrigAsset,
		// XMRig archives: xmrig-{ver}-linux-static-x64/xmrig
		extract: extractFirstMatch,
	})
}

// EnsureP2Pool ensures the p2pool binary is present in binDir.
func EnsureP2Pool(binDir string) (string, error) {
	return ensure(binDir, dep{
		name:  "p2pool",
		repo:  "SChernykh/p2pool",
		asset: p2poolAsset,
		// p2pool archives: p2pool-v{ver}-linux-x64/p2pool
		extract: extractFirstMatch,
	})
}

// ---- internals ----

type dep struct {
	name    string
	repo    string
	asset   func(version string) (url string, isZip bool)
	extract func(data []byte, name string, isZip bool) ([]byte, error)
}

func binName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func ensure(binDir string, d dep) (string, error) {
	dest := filepath.Join(binDir, binName(d.name))
	if _, err := os.Stat(dest); err == nil {
		return dest, nil // already present
	}

	fmt.Printf("kind-miner: %s not found — downloading automatically…\n", d.name)

	version, err := latestRelease(d.repo)
	if err != nil {
		return "", fmt.Errorf("could not check latest %s release: %w\nYou can also install %s manually and place it in %s", d.name, err, d.name, binDir)
	}
	fmt.Printf("  → latest %s: %s\n", d.name, version)

	url, isZip := d.asset(version)
	fmt.Printf("  → downloading from %s\n", url)

	data, err := download(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w\nIf this keeps failing, download %s manually and place the binary in %s", d.name, err, d.name, binDir)
	}

	binary, err := d.extract(data, binName(d.name), isZip)
	if err != nil {
		return "", fmt.Errorf("extracting %s binary: %w", d.name, err)
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("creating bin dir: %w", err)
	}
	if err := os.WriteFile(dest, binary, 0o755); err != nil {
		return "", fmt.Errorf("saving %s: %w", d.name, err)
	}

	fmt.Printf("  ✓ %s installed to %s\n", d.name, dest)
	return dest, nil
}

// latestRelease follows the GitHub /releases/latest redirect and extracts
// the version tag (e.g. "v6.21.3" → "6.21.3").
func latestRelease(repo string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // stop at first redirect
		},
	}
	resp, err := client.Get("https://github.com/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect from GitHub releases/latest")
	}
	// Location: https://github.com/owner/repo/releases/tag/v1.2.3
	parts := strings.Split(loc, "/")
	tag := parts[len(parts)-1] // "v1.2.3"
	return strings.TrimPrefix(tag, "v"), nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	// Stream with a simple progress indicator.
	total := resp.ContentLength
	var buf bytes.Buffer
	buf.Grow(int(max64(total, 1024*1024)))

	pr := &progressReader{r: resp.Body, total: total}
	if _, err := io.Copy(&buf, pr); err != nil {
		return nil, err
	}
	fmt.Println() // end the progress line
	return buf.Bytes(), nil
}

type progressReader struct {
	r       io.Reader
	total   int64
	read    int64
	lastPct int
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.total > 0 {
		pct := int(p.read * 100 / p.total)
		if pct/10 > p.lastPct/10 {
			fmt.Printf("  %3d%%\r", pct)
			p.lastPct = pct
		}
	}
	return n, err
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// extractFirstMatch finds the first file whose base name matches `name`
// inside a tar.gz or zip archive.
func extractFirstMatch(data []byte, name string, isZip bool) ([]byte, error) {
	if isZip {
		return extractZip(data, name)
	}
	return extractTarGz(data, name)
}

func extractTarGz(data []byte, name string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", name)
}

func extractZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in zip archive", name)
}

// ---- asset URL builders ----

func xmrigAsset(version string) (string, bool) {
	base := fmt.Sprintf("https://github.com/xmrig/xmrig/releases/download/v%s/", version)
	switch {
	case runtime.GOOS == "windows":
		return base + fmt.Sprintf("xmrig-%s-msvc-win64.zip", version), true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return base + fmt.Sprintf("xmrig-%s-macos-arm64.tar.gz", version), false
	case runtime.GOOS == "darwin":
		return base + fmt.Sprintf("xmrig-%s-macos-x64.tar.gz", version), false
	case runtime.GOARCH == "arm64":
		return base + fmt.Sprintf("xmrig-%s-linux-static-aarch64.tar.gz", version), false
	default: // linux amd64
		return base + fmt.Sprintf("xmrig-%s-linux-static-x64.tar.gz", version), false
	}
}

func p2poolAsset(version string) (string, bool) {
	base := fmt.Sprintf("https://github.com/SChernykh/p2pool/releases/download/v%s/", version)
	switch {
	case runtime.GOOS == "windows":
		return base + fmt.Sprintf("p2pool-v%s-windows-x64.zip", version), true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return base + fmt.Sprintf("p2pool-v%s-macos-aarch64.tar.gz", version), false
	case runtime.GOOS == "darwin":
		return base + fmt.Sprintf("p2pool-v%s-macos-x64.tar.gz", version), false
	case runtime.GOARCH == "arm64":
		return base + fmt.Sprintf("p2pool-v%s-linux-aarch64.tar.gz", version), false
	default: // linux amd64
		return base + fmt.Sprintf("p2pool-v%s-linux-x64.tar.gz", version), false
	}
}
