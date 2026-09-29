package core

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/rollout"
)

// Every packaged release ships its engines: the AppImage and the Flatpak in
// <prefix>/lib/kind-miner/engines beside <prefix>/bin/kind-miner, the .deb in
// /opt/kind-miner/engines beside the daemon. A build that finds them there
// downloads nothing, ever. First-run downloads told GitHub a miner had been
// installed before Tor was up, failed wherever antivirus flags Monero
// binaries, and never moved without an app release anyway. Only a build with
// no engines beside it — go run, a bare binary — still downloads the pinned
// ones.

// binaries are the engine paths this run uses. They are kept out of the
// config on purpose: an AppImage's engines live under a mount point that
// changes every launch, and a path saved from one launch is gone by the next.
type binaries struct {
	xmrig, p2pool string
	// bundle is the packaged pair, if this build has one; its Tor, when it
	// ships one, is bundle.Tor.
	bundle *rollout.Engines
	// packaged: engines came with the build, so nothing is downloaded.
	packaged bool
}

// bundleDirs are where a packaged build keeps its engines, relative to the
// executable.
func bundleDirs(exe string) []string {
	dir := filepath.Dir(exe)
	return []string{
		filepath.Join(dir, "engines"),                            // .deb: /opt/kind-miner
		filepath.Join(dir, "..", "lib", "kind-miner", "engines"), // AppImage, Flatpak: <prefix>/bin
	}
}

// findBundle returns the engines packaged with the executable at exe, or nil.
func findBundle(exe string) *rollout.Engines {
	for _, dir := range bundleDirs(exe) {
		e, err := rollout.Load(dir)
		if err != nil {
			log.Printf("warning: the bundled engines in %s cannot be used: %v", dir, err)
			continue
		}
		if e != nil {
			return e
		}
	}
	return nil
}

// ownPath is a path from the config that names the user's own build. The
// managed download directory is not one: earlier versions wrote the
// downloaded path back into the config, and honouring it now would pin those
// users to whatever was downloaded then.
func ownPath(p string) string {
	if p == "" || strings.HasPrefix(p, autoinstall.BinDir()) {
		return ""
	}
	return p
}

// resolveEngines decides which xmrig and p2pool to run. In order: the pair
// the daemon chose (UseEngines — its rollback must win), the user's own
// builds from the config, the bundled pair, and only in an unpackaged build
// the pinned download.
func (s *Supervisor) resolveEngines(exe string, needP2Pool bool, emit func(Step)) error {
	b := binaries{bundle: findBundle(exe)}
	b.packaged = b.bundle != nil || s.engines.xmrig != ""

	pick := func(chosen, own, bundled string, ensure func(string) (string, error), name string) (string, error) {
		switch {
		case chosen != "":
			return chosen, nil
		case own != "":
			return own, nil
		case bundled != "":
			return bundled, nil
		case b.packaged:
			return "", fmt.Errorf("this build of kind-miner should have come with %s, and it is missing: reinstall it", name)
		}
		return ensure(autoinstall.BinDir())
	}
	var bundledX, bundledP string
	if b.bundle != nil {
		bundledX, bundledP = b.bundle.XMRig, b.bundle.P2Pool
	}

	emit(StepInstallXMRig)
	var err error
	if b.xmrig, err = pick(s.engines.xmrig, ownPath(s.cfg.XMRigBinPath), bundledX, autoinstall.EnsureXMRig, "xmrig"); err != nil {
		return err
	}
	if needP2Pool {
		emit(StepInstallP2Pool)
		if b.p2pool, err = pick(s.engines.p2pool, ownPath(s.cfg.P2PoolBinPath), bundledP, autoinstall.EnsureP2Pool, "p2pool"); err != nil {
			return err
		}
	}
	s.bin = b
	return nil
}

// errNoTor is returned by torBinary for a packaged build with no Tor of its
// own and none installed.
var errNoTor = errors.New("no tor: this build does not ship one and none is installed — install tor")

// torBinary finds a tor to run: the config's, the bundled one, one beside the
// executable or on PATH, and only in an unpackaged build the pinned download.
func (s *Supervisor) torBinary() (string, error) {
	if s.cfg.TorBinPath != "" {
		return engine.FindTor(s.cfg.TorBinPath)
	}
	if b := s.bin.bundle; b != nil && b.Tor != "" {
		return b.Tor, nil
	}
	if bin, err := engine.FindTor(""); err == nil {
		return bin, nil
	}
	if s.bin.packaged {
		return "", errNoTor
	}
	return autoinstall.EnsureTor(autoinstall.BinDir())
}
