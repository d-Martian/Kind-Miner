package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
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

	shots := []struct {
		name    string
		content fyne.CanvasObject
	}{
		{"onboarding-welcome", welcomeScreen(func() {})},
		{"onboarding-wallet", walletScreen(nil, func(string) {})},
	}

	for _, s := range shots {
		w := test.NewWindow(s.content)
		w.Resize(fyne.NewSize(440, 320))
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
