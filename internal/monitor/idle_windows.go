package monitor

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount64   = kernel32.NewProc("GetTickCount64")
)

// lastInputInfo mirrors the Win32 LASTINPUTINFO struct.
type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

func probeIdleBackend() idleBackend {
	b := &winIdle{}
	if _, err := b.idleTime(); err != nil {
		log.Printf("idle detection: unavailable (%v); mining will not wait for you to go idle", err)
		return nil
	}
	log.Printf("idle detection: using %s", b.name())
	return b
}

type winIdle struct{}

func (w *winIdle) name() string { return "GetLastInputInfo" }

func (w *winIdle) idleTime() (time.Duration, error) {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	r, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, fmt.Errorf("GetLastInputInfo: %w", err)
	}
	ticks, _, _ := procGetTickCount64.Call()

	// dwTime is a 32-bit tick count that wraps every ~49.7 days, while
	// GetTickCount64 does not. Truncating the 64-bit value to 32 bits puts both
	// on the same wrapping clock, so the subtraction stays correct across a wrap.
	elapsed := uint32(ticks) - info.dwTime
	return time.Duration(elapsed) * time.Millisecond, nil
}
