package gui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/d-Martian/Kind-Miner/internal/config"
	"github.com/d-Martian/Kind-Miner/internal/core"
	"github.com/d-Martian/Kind-Miner/internal/engine"
	"github.com/d-Martian/Kind-Miner/internal/scheduler"
	"github.com/d-Martian/Kind-Miner/internal/stats"
)

// TestCaptureScreenshots renders the onboarding screens to PNGs for the
// AppStream metainfo (<screenshots>). It is a capture utility, not an
// assertion: it only runs when KM_SHOT_DIR points at an output directory, so it
// stays out of normal `go test` runs. Software-rendered via Fyne's test driver,
// so it needs no display and has no side effects (no supervisor, no network).
//
// Regenerate from the repo root — the path must be absolute because `go test`
// runs in the package directory, not where you invoked it:
//
//	KM_SHOT_DIR="$PWD/assets/screenshots" \
//	    go test -mod=mod ./internal/gui -run TestCaptureScreenshots
func TestCaptureScreenshots(t *testing.T) {
	dir := os.Getenv("KM_SHOT_DIR")
	if dir == "" {
		t.Skip("set KM_SHOT_DIR to capture screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	a := test.NewApp()
	a.Settings().SetTheme(kindTheme{})

	// Built without a supervisor: the setup screens only read the config, and
	// nothing here reaches the code that would need a running miner.
	cfg := config.Defaults()
	cfg.Wallet = sampleAddress

	shots := []struct {
		name    string
		size    fyne.Size
		content func() fyne.CanvasObject
	}{
		{"onboarding", onboardingSize, onboardingPane(cfg)},
		{"dashboard", windowSize, func() fyne.CanvasObject {
			// A supervisor that was never started: Config answers, Scheduler
			// does not, so the dashboard renders its resting state. The chart
			// is then fed a synthetic history, because a shot of an empty plot
			// would show none of what the screen is for.
			u := &uiApp{sup: core.New(cfg)}
			content := u.dashboardScreen()
			history := demoHistory()
			// The two-minute window, so the burst of work and the recovery
			// after it both fit in the frame.
			opts := cfg.Chart
			opts.WindowSeconds = 120
			u.dash.chart.Set(history, cfg.Preset().Ceiling, opts)
			demoMining(u.dash, history, cfg)
			return content
		}},
	}

	for _, s := range shots {
		w := test.NewWindow(s.content())
		w.Resize(s.size)
		img := w.Canvas().Capture()

		path := filepath.Join(dir, s.name+".png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("encode %s: %v", path, err)
		}
		f.Close()
		b := img.Bounds()
		t.Logf("wrote %s (%dx%d)", path, b.Dx(), b.Dy())
		w.Close()
	}
}

// demoHistory is a plausible minute of a desktop being used: a quiet stretch,
// a burst of work, and the miner giving the machine back and creeping return.
// It exists so the captured screenshot shows the behaviour the chart is there
// to demonstrate, rather than a flat line.
func demoHistory() []stats.Sample {
	const n = 60
	now := time.Now()
	out := make([]stats.Sample, 0, n)
	allowed := 0.62
	for i := 0; i < n; i++ {
		other := 0.09
		switch {
		case i >= 18 && i < 26:
			other = 0.71 // a build, a page load, something real
		case i >= 40 && i < 44:
			other = 0.44
		}
		target := 0.78 - other
		if target < 0 {
			target = 0
		}
		prev := allowed
		if target < allowed {
			allowed += (target - allowed) * 0.9
		} else {
			allowed += (target - allowed) * 0.15
		}
		out = append(out, stats.Sample{
			At:       now.Add(time.Duration(i-n+1) * 2 * time.Second),
			OtherCPU: other,
			MinerCPU: allowed,
			Hashrate: allowed * 9500,
			TempC:    46 + (other+allowed)*30,
			// A laptop-class package: idle floor plus a share for load.
			Watts:      6 + (other+allowed)*32,
			WattsKnown: true,
			BackedOff:  prev-allowed > 0.07,
		})
	}
	return out
}

// demoMining fills the header and the tiles as a running miner would. With no
// scheduler or p2pool behind it the dashboard showed "Starting…" over a row
// of dashes, which is what the README and the store listing then showed: the
// one image a newcomer sees, telling them nothing about what it does. Every
// figure goes through the code the live screen uses, from the demo history;
// only the network's difficulty and reward, which a shot cannot ask p2pool
// for, are stated here.
func demoMining(d *dashboard, history []stats.Sample, cfg *config.Config) {
	text, tint := statusSummary(scheduler.StateFull, "", false, scheduler.OverrideNone, cfg.Preset(),
		func() (int, bool) { return 0, false }, coreCounts{})
	d.pill.Set(text, tint)
	setText(d.uptime, "up "+formatUptime(3*time.Hour+12*time.Minute))
	setVisible(d.mineNow, skipWaitShown(scheduler.OverrideNone, false))

	hashrate, _ := stats.Mean(history, hashrateWindow, stats.Hashrate)
	value, unit := hashrateParts(hashrate)
	d.hash.Set(value, unit)

	// Roughly the network of late 2026: about 5 GH/s, so a difficulty of
	// 120 s of that, and a 0.6 XMR tail-emission-era block.
	pool := engine.P2PoolStats{NetworkDifficulty: 600_000_000_000, BlockReward: 600_000_000_000}
	if mean, ok := stats.Mean(history, time.Minute, stats.Hashrate); ok {
		if perDay, ok := pool.EstimatedXMRPerDay(mean); ok {
			d.income.Set(formatXMR(perDay, 5), "")
		}
	}

	d.refreshPower(history)
	d.link.Set("Connected", "")
	d.link.SetColor(colorOK)
}

// onboardingPane builds the setup screen in isolation, without a supervisor —
// it only reads the config.
func onboardingPane(cfg *config.Config) func() fyne.CanvasObject {
	return func() fyne.CanvasObject {
		o := &onboarding{cfg: cfg}
		o.build()
		return o.object
	}
}

// TestScreensRender lays out every screen through Fyne's software renderer.
// It asserts nothing about how they look — it catches the layout panicking, a
// nil widget, or a container sized to zero, none of which the unit tests above
// would ever reach.
func TestScreensRender(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(kindTheme{})

	cfg := config.Defaults()
	cfg.Wallet = sampleAddress
	u := &uiApp{sup: core.New(cfg)}

	screens := map[string]fyne.CanvasObject{
		"onboarding": onboardingPane(cfg)(),
		"dashboard":  u.dashboardScreen(),
		"progress":   u.progressScreen(core.StepStartP2Pool),
		"error":      u.errorScreen(errRender),
	}

	for name, content := range screens {
		t.Run(name, func(t *testing.T) {
			w := test.NewWindow(content)
			defer w.Close()
			w.Resize(windowSize)

			img := w.Canvas().Capture()
			if img == nil {
				t.Fatal("canvas captured nothing")
			}
			if b := img.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
				t.Fatalf("screen rendered at %dx%d", b.Dx(), b.Dy())
			}
		})
	}
}

// The dashboard refreshes once a second against whatever the supervisor has.
// Before mining starts that is nothing at all, and it must cope rather than
// dereference its way into a panic.
func TestDashboardRefreshBeforeStartup(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(kindTheme{})

	u := &uiApp{sup: core.New(config.Defaults())}
	d := u.newDashboard()
	d.refresh()
	u.updateDashboard()
}

var errRender = errors.New("p2pool exited before becoming ready; last output: could not connect to 127.0.0.1:18081")

// sampleAddress is a valid primary address, so the captured screens show the
// "valid" state rather than a validation error. It has to be a real one —
// the check verifies the checksum — so it is the Monero General Fund's
// published donation address.
const sampleAddress = "44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A"

// sampleSubaddress is sampleAddress's keys under the subaddress tag, with its
// checksum recomputed: a well-formed subaddress no wallet made, and nowhere
// to send coins.
const sampleSubaddress = "84zPbCjb38gBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGMwZRBo"
