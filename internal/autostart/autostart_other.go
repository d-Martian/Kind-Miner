//go:build !linux && !freebsd && !openbsd && !netbsd && !darwin && !windows

package autostart

import "errors"

func enable(Options) error          { return errors.ErrUnsupported }
func disable(Options) error         { return errors.ErrUnsupported }
func enabled(Options) (bool, error) { return false, errors.ErrUnsupported }
