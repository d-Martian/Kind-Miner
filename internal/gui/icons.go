package gui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icons/tray-mining.png
var iconMiningPNG []byte

//go:embed icons/tray-throttle.png
var iconThrottlePNG []byte

//go:embed icons/tray-paused.png
var iconPausedPNG []byte

//go:embed icons/tray-warning.png
var iconWarningPNG []byte

// System-tray icon resources, one per mining state.
var (
	resMining   = fyne.NewStaticResource("tray-mining.png", iconMiningPNG)
	resThrottle = fyne.NewStaticResource("tray-throttle.png", iconThrottlePNG)
	resPaused   = fyne.NewStaticResource("tray-paused.png", iconPausedPNG)
	// resWarning is the stopped icon with an amber badge: mining is held for a
	// fault the user may need to look at, not stood down for their sake.
	resWarning = fyne.NewStaticResource("tray-warning.png", iconWarningPNG)
)
