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

// System-tray icon resources, one per mining state.
var (
	resMining   = fyne.NewStaticResource("tray-mining.png", iconMiningPNG)
	resThrottle = fyne.NewStaticResource("tray-throttle.png", iconThrottlePNG)
	resPaused   = fyne.NewStaticResource("tray-paused.png", iconPausedPNG)
)
