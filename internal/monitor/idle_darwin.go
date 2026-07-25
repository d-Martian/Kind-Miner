//go:build darwin && cgo

package monitor

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

import (
	"fmt"
	"log"
	"time"
)

// kCGAnyInputEventType is ~0 — "any input event", not exported as a constant by
// the CoreGraphics headers in a form cgo can reach.
const anyInputEventType = C.CGEventType(^C.uint32_t(0))

func probeIdleBackend() idleBackend {
	b := &darwinIdle{}
	if _, err := b.idleTime(); err != nil {
		log.Printf("idle detection: unavailable (%v); mining will not wait for you to go idle", err)
		return nil
	}
	log.Printf("idle detection: using %s", b.name())
	return b
}

type darwinIdle struct{}

func (d *darwinIdle) name() string { return "CGEventSourceSecondsSinceLastEventType" }

// idleTime asks the window server how long ago the last input event arrived.
//
// This needs no Accessibility or Input Monitoring permission, unlike an event
// tap — it reports only elapsed time, never event contents, so macOS does not
// gate it behind a TCC prompt.
func (d *darwinIdle) idleTime() (time.Duration, error) {
	secs := float64(C.CGEventSourceSecondsSinceLastEventType(
		C.kCGEventSourceStateCombinedSessionState,
		anyInputEventType,
	))
	if secs < 0 {
		return 0, fmt.Errorf("CGEventSourceSecondsSinceLastEventType returned %v", secs)
	}
	return time.Duration(secs * float64(time.Second)), nil
}
