package gui

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/stats"
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
		{"onboarding-wallet", fyne.NewSize(560, 440), onboardingPane(cfg, 1)},
		{"onboarding-connection", fyne.NewSize(560, 440), onboardingPane(cfg, 2)},
		{"onboarding-binaries", fyne.NewSize(560, 440), onboardingPane(cfg, 3)},
		{"onboarding-kindness", fyne.NewSize(560, 440), onboardingPane(cfg, 4)},
		{"dashboard", windowSize, func() fyne.CanvasObject {
			// A supervisor that was never started: Config answers, Scheduler
			// does not, so the dashboard renders its resting state. The chart
			// is then fed a synthetic history, because a shot of an empty plot
			// would show none of what the screen is for.
			u := &uiApp{sup: core.New(cfg)}
			content := u.dashboardScreen()
			// The two-minute window, so the burst of work and the recovery
			// after it both fit in the frame.
			opts := cfg.Chart
			opts.WindowSeconds = 120
			u.dash.chart.Set(demoHistory(), cfg.Preset().Ceiling, opts)
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
			At:        now.Add(time.Duration(i-n+1) * 2 * time.Second),
			OtherCPU:  other,
			MinerCPU:  allowed,
			Hashrate:  allowed * 9500,
			TempC:     46 + (other+allowed)*30,
			BackedOff: prev-allowed > 0.07,
		})
	}
	return out
}

// onboardingPane builds one setup step in isolation, without a supervisor —
// the screens only read the config.
func onboardingPane(cfg *config.Config, step int) func() fyne.CanvasObject {
	return func() fyne.CanvasObject {
		o := &onboarding{cfg: cfg, step: step}
		o.build()
		o.show()
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
		"onboarding": onboardingPane(cfg, 1)(),
		"dashboard":  u.dashboardScreen(),
		"progress":   u.progressScreen(core.StepStartP2Pool),
		"error":      u.errorScreen(errRender),
	}
	for i := 1; i <= onboardingSteps; i++ {
		screens[fmt.Sprintf("onboarding-step-%d", i)] = onboardingPane(cfg, i)()
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

// sampleAddress is a syntactically valid primary address used so the captured
// screens show the "looks good" state rather than a validation error. It is a
// well-known example address, not anywhere anyone should send coins.
const sampleAddress = "4At3X5rvVypTofgmueN9s9QtrzdRe5BueFrskAZi17BoYbhzysozzoMFB6zWnTKdGC6AxEAbEE5czFR3hbEEJbsm4hVwCJk"
