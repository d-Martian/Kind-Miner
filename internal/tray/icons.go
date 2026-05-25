package tray

import _ "embed"

//go:embed icons/tray-mining.png
var iconMining []byte

//go:embed icons/tray-throttle.png
var iconThrottle []byte

//go:embed icons/tray-paused.png
var iconPaused []byte

// iconSetup reuses the mining icon; setup state is communicated via tooltip.
var iconSetup = iconMining
