// Command stage-engines assembles the engines a desktop package ships: the
// xmrig built from source (scripts/build-xmrig.sh), and the pinned p2pool and
// Tor Expert Bundle, downloaded and checked against the same pins kind-miner
// itself would verify at run time.
//
//	go run ./tools/stage-engines -xmrig dist/xmrig/linux-amd64/xmrig -out build/engines
//
// It writes, in -out:
//
//	xmrig  p2pool  tor/tor (and its libraries)  engines.json
//
// which scripts/build-appimage.sh and scripts/build-flatpak.sh copy into
// <prefix>/lib/kind-miner/engines, where kind-miner finds them and then never
// downloads anything. The upstream xmrig is not an option here: it keeps a
// 1% donation that connects outside Tor.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
)

func main() {
	xmrig := flag.String("xmrig", "", "xmrig built from source by scripts/build-xmrig.sh")
	out := flag.String("out", "", "directory to stage the engines in")
	flag.Parse()
	if *xmrig == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := stage(*xmrig, *out); err != nil {
		fmt.Fprintf(os.Stderr, "stage-engines: %v\n", err)
		os.Exit(1)
	}
}

func stage(xmrig, out string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := copyFile(xmrig, filepath.Join(out, "xmrig"), 0o755); err != nil {
		return fmt.Errorf("xmrig: %w", err)
	}
	p2pool, err := autoinstall.EnsureP2Pool(out)
	if err != nil {
		return err
	}
	if _, err := autoinstall.EnsureTor(out); err != nil {
		return err
	}
	if err := trimTor(filepath.Join(out, "tor")); err != nil {
		return err
	}
	// The stamp is how a download directory knows what it holds; a bundle
	// says so in engines.json instead.
	os.Remove(p2pool + ".pin.json")

	xv, pv, tv := autoinstall.PinnedVersions()
	xs, err := sum(filepath.Join(out, "xmrig"))
	if err != nil {
		return err
	}
	ps, err := sum(p2pool)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{
		"xmrig": xv, "p2pool": pv, "tor": tv,
		"xmrig_sha256": xs, "p2pool_sha256": ps,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "engines.json"), append(data, '\n'), 0o644)
}

// trimTor leaves the bundle's Tor with what kind-miner runs. The pluggable
// transports are for bridges, which kind-miner never configures, and would
// add their megabytes to every download. The libraries come out of the
// archive owner-only; a Flatpak installed system-wide is owned by root, and
// its tor could not then load them.
func trimTor(dir string) error {
	if err := os.RemoveAll(filepath.Join(dir, "pluggable_transports")); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(path, 0o755)
	})
}

func sum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	o, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}
