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

// BinDir returns the directory where bundled binaries are stored.
// Inside a Flatpak sandbox the exe lives under /app/bin (read-only), so we
// redirect to XDG_DATA_HOME instead.
func BinDir() string {
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			home, _ := os.UserHomeDir()
			dataHome = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(dataHome, "kind-miner", "bin")
	}
	exe, err := os.Executable()
	if err != nil {
		return "bin"
	}
	return filepath.Join(filepath.Dir(exe), "bin")
}

// EnsureXMRig ensures the xmrig binary is present in binDir.
func EnsureXMRig(binDir string) (string, error) {
	return ensure(binDir, dep{
		name:    "xmrig",
		repo:    "xmrig/xmrig",
		asset:   xmrigAsset,
		extract: extractFirstMatch,
	})
}

// EnsureP2Pool ensures the p2pool binary is present in binDir.
func EnsureP2Pool(binDir string) (string, error) {
	return ensure(binDir, dep{
		name:    "p2pool",
		repo:    "SChernykh/p2pool",
		asset:   p2poolAsset,
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
		return dest, nil // already present — silent
	}

	// Print initial status on a single line; subsequent updates overwrite it.
	printStatus(d.name, "", "  checking…", false)

	version, err := latestRelease(d.repo)
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("could not check latest %s release: %w\nInstall %s manually and place it in %s", d.name, err, d.name, binDir)
	}

	url, isZip := d.asset(version)
	label := fmt.Sprintf("  ↓ %-8s  v%s", d.name, version)

	data, err := downloadWithProgress(label, url)
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("downloading %s: %w\nInstall %s manually and place the binary in %s", d.name, err, d.name, binDir)
	}

	binary, err := d.extract(data, binName(d.name), isZip)
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("extracting %s: %w", d.name, err)
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		fmt.Println()
		return "", fmt.Errorf("creating bin dir: %w", err)
	}
	if err := os.WriteFile(dest, binary, 0o755); err != nil {
		fmt.Println()
		return "", fmt.Errorf("saving %s: %w", d.name, err)
	}

	printStatus(d.name, version, fmtSize(int64(len(binary))), true)
	return dest, nil
}

// printStatus overwrites the current terminal line with a status entry.
// done=true prints a ✓ and advances to the next line; done=false uses \r.
func printStatus(name, version, detail string, done bool) {
	var line string
	if version != "" {
		line = fmt.Sprintf("  %s %-8s  v%s  %s",
			map[bool]string{true: "✓", false: "↓"}[done],
			name, version, detail)
	} else {
		line = fmt.Sprintf("  ↓ %-8s  %s", name, detail)
	}
	if done {
		fmt.Printf("\r%-72s\n", line)
	} else {
		fmt.Printf("\r%-72s\r", line)
	}
}

// downloadWithProgress downloads url and prints a live progress bar prefixed
// with label. It leaves the cursor at the start of the current line so the
// caller can overwrite it with a completion line.
func downloadWithProgress(label, url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	total := resp.ContentLength
	var buf bytes.Buffer
	if total > 0 {
		buf.Grow(int(total))
	}

	pr := &progressReader{r: resp.Body, total: total, label: label}
	if _, err := io.Copy(&buf, pr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	label      string
	lastPrint  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if time.Since(p.lastPrint) >= 80*time.Millisecond {
		p.printBar()
		p.lastPrint = time.Now()
	}
	return n, err
}

func (p *progressReader) printBar() {
	const barWidth = 22
	var line string
	if p.total > 0 {
		pct := int(p.read * 100 / p.total)
		filled := pct * barWidth / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		line = fmt.Sprintf("%s  [%s]  %3d%%  %s", p.label, bar, pct, fmtSize(p.read))
	} else {
		line = fmt.Sprintf("%s  %s", p.label, fmtSize(p.read))
	}
	fmt.Printf("\r%-72s\r", line)
}

func fmtSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%d KB", b>>10)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// latestRelease follows the GitHub /releases/latest redirect and returns the
// version tag without the leading "v" (e.g. "6.21.3").
func latestRelease(repo string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
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
	parts := strings.Split(loc, "/")
	return strings.TrimPrefix(parts[len(parts)-1], "v"), nil
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
	default:
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
	default:
		return base + fmt.Sprintf("p2pool-v%s-linux-x64.tar.gz", version), false
	}
}
