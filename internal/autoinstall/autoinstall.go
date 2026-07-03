// Package autoinstall downloads XMRig and p2pool on first run so users never
// have to manually install anything.
package autoinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
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
// When running inside a Flatpak or AppImage the executable path is read-only,
// so both cases redirect to XDG_DATA_HOME instead.
func BinDir() string {
	// Flatpak: exe lives under /app/bin (read-only squashfs)
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		return xdgDataBin()
	}
	// AppImage: exe is mounted read-only under /tmp/.mount_*/usr/bin/
	if os.Getenv("APPIMAGE") != "" {
		return xdgDataBin()
	}
	exe, err := os.Executable()
	if err != nil {
		return "bin"
	}
	return filepath.Join(filepath.Dir(exe), "bin")
}

func xdgDataBin() string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "kind-miner", "bin")
}

// EnsureXMRig ensures the xmrig binary is present in binDir, downloading the
// pinned release and verifying it against the SHA256 in deps.json.
func EnsureXMRig(binDir string) (string, error) {
	return ensurePinned(binDir, "xmrig", deps.XMRig)
}

// EnsureP2Pool ensures the p2pool binary is present in binDir, downloading the
// pinned release and verifying it against the SHA256 in deps.json.
func EnsureP2Pool(binDir string) (string, error) {
	return ensurePinned(binDir, "p2pool", deps.P2Pool)
}

// ---- pinned dependency manifest (deps.json) ----

// XMRig and p2pool are downloaded at runtime (the Flatpak/AppImage ship no
// bundled copy), so — like the Tor Expert Bundle — their archives are pinned to
// a specific version and verified against a known SHA256. deps.json is the
// single source of truth for those pins, shared with the release packaging
// scripts and maintained by the monero-miner-dependency-updater skill.

//go:embed deps.json
var depsJSON []byte

type depAsset struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type depSpec struct {
	Repo        string              `json:"repo"`
	Version     string              `json:"version"`
	URLTemplate string              `json:"url_template"`
	Assets      map[string]depAsset `json:"assets"`
}

type depManifest struct {
	XMRig  depSpec `json:"xmrig"`
	P2Pool depSpec `json:"p2pool"`
}

// deps is parsed once at init so a malformed deps.json fails fast and loudly at
// startup rather than at first download.
var deps = mustParseDeps()

func mustParseDeps() depManifest {
	var m depManifest
	if err := json.Unmarshal(depsJSON, &m); err != nil {
		panic("autoinstall: invalid deps.json: " + err.Error())
	}
	return m
}

func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// assetURL renders a dependency's download URL from its url_template, filling
// the {version} and {file} placeholders.
func assetURL(spec depSpec, asset depAsset) string {
	return strings.NewReplacer("{version}", spec.Version, "{file}", asset.File).Replace(spec.URLTemplate)
}

// ---- internals ----

func binName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// ensurePinned downloads, verifies, and extracts a pinned dependency binary
// into binDir if it is not already present. The archive is checked against the
// SHA256 pinned in deps.json before extraction — the same tamper-evident,
// reproducible pattern used for the Tor Expert Bundle.
func ensurePinned(binDir, name string, spec depSpec) (string, error) {
	dest := filepath.Join(binDir, binName(name))
	if _, err := os.Stat(dest); err == nil {
		return dest, nil // already present — silent
	}

	key := platformKey()
	asset, ok := spec.Assets[key]
	if !ok {
		return "", fmt.Errorf("no pinned %s build for %s — install %s manually and place it in %s", name, key, name, binDir)
	}

	// Print initial status on a single line; subsequent updates overwrite it.
	printStatus(name, "", "  checking…", false)

	url := assetURL(spec, asset)
	label := fmt.Sprintf("  ↓ %-8s  v%s", name, spec.Version)

	data, err := downloadWithProgress(label, url)
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("downloading %s: %w\nInstall %s manually and place the binary in %s", name, err, name, binDir)
	}

	binary, err := verifyAndExtract(data, binName(name), asset.SHA256, strings.HasSuffix(asset.File, ".zip"))
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("%s: %w", name, err)
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		fmt.Println()
		return "", fmt.Errorf("creating bin dir: %w", err)
	}
	if err := os.WriteFile(dest, binary, 0o755); err != nil {
		fmt.Println()
		return "", fmt.Errorf("saving %s: %w", name, err)
	}

	printStatus(name, spec.Version, fmtSize(int64(len(binary))), true)
	return dest, nil
}

// verifyAndExtract checks data against wantSHA (a hex-encoded SHA256) and, on a
// match, returns the named binary extracted from the archive. A mismatch is a
// hard error — the download is never trusted on the strength of its URL alone.
func verifyAndExtract(data []byte, name, wantSHA string, isZip bool) ([]byte, error) {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantSHA {
		return nil, fmt.Errorf("download failed verification:\n  got  %s\n  want %s", got, wantSHA)
	}
	return extractFirstMatch(data, name, isZip)
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

// ---- Tor Expert Bundle ----

// torVersion pins the Tor Expert Bundle release. The per-platform SHA256 sums
// below come from the build manifest at:
//   archive.torproject.org/tor-package-archive/torbrowser/15.0.15/sha256sums-unsigned-build.txt
// Tor is security-critical, so unlike XMRig/p2pool its download is verified
// against these pinned hashes.
const torVersion = "15.0.15"

type torDist struct {
	file   string
	sha256 string
}

func torDistFor() (torDist, bool) {
	v := torVersion
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return torDist{"tor-expert-bundle-linux-x86_64-" + v + ".tar.gz", "ffc4528394442c3b33a9ccece3536511a3992c78e704756693bed7a2297ef0e7"}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return torDist{"tor-expert-bundle-macos-aarch64-" + v + ".tar.gz", "9afb993d5d505a1cfb62d3119c25cf07674d7e9305a9a87116dcdff36c64e054"}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return torDist{"tor-expert-bundle-macos-x86_64-" + v + ".tar.gz", "664ba99389b73bc4264b0ec1dfec247b444e4ce664ea7e19d4b58081bc87cf3c"}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return torDist{"tor-expert-bundle-windows-x86_64-" + v + ".tar.gz", "8d3daf579192f3f128c0f42553dd994c640501b4b98682216d807c88004f7a96"}, true
	}
	return torDist{}, false
}

// EnsureTor downloads and verifies the Tor Expert Bundle into binDir/tor on
// first use, returning the path to the tor binary. The download is checked
// against a pinned SHA256 (see torVersion).
func EnsureTor(binDir string) (string, error) {
	torDir := filepath.Join(binDir, "tor")
	dest := filepath.Join(torDir, binName("tor"))
	if _, err := os.Stat(dest); err == nil {
		return dest, nil // already present
	}

	dist, ok := torDistFor()
	if !ok {
		return "", fmt.Errorf("no Tor Expert Bundle for %s/%s — install tor and put it on PATH", runtime.GOOS, runtime.GOARCH)
	}

	url := "https://archive.torproject.org/tor-package-archive/torbrowser/" + torVersion + "/" + dist.file
	data, err := downloadWithProgress(fmt.Sprintf("  ↓ %-8s  v%s", "tor", torVersion), url)
	if err != nil {
		fmt.Println()
		return "", fmt.Errorf("downloading tor: %w", err)
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != dist.sha256 {
		fmt.Println()
		return "", fmt.Errorf("tor download failed verification:\n  got  %s\n  want %s", got, dist.sha256)
	}

	if err := extractTorBundle(data, torDir); err != nil {
		fmt.Println()
		return "", fmt.Errorf("extracting tor: %w", err)
	}

	printStatus("tor", torVersion, fmtSize(int64(len(data))), true)
	return dest, nil
}

// extractTorBundle writes the `tor/` subtree of the Expert Bundle (the binary
// plus its bundled libraries and pluggable transports) into destDir. The tor
// binary finds its sibling libraries through an $ORIGIN rpath.
func extractTorBundle(data []byte, destDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gr.Close()

	cleanDest := filepath.Clean(destDir)
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(hdr.Name, "tor/")
		if rel == hdr.Name || rel == "" {
			continue // keep only the tor/ subtree
		}
		target := filepath.Join(destDir, rel)
		if !strings.HasPrefix(filepath.Clean(target), cleanDest+string(os.PathSeparator)) {
			continue // guard against path traversal in the archive
		}
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(hdr.Mode).Perm()
		if mode == 0 {
			mode = 0o644
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	if err := os.Chmod(filepath.Join(destDir, binName("tor")), 0o755); err != nil {
		return fmt.Errorf("tor binary missing from bundle: %w", err)
	}
	return nil
}
