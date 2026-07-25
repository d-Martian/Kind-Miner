package autostart

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user "start these at login" list. HKCU (not HKLM) keeps the
// change to the current user and needs no elevation.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// valueName is the registry value under runKey. Keyed by app id so an entry
// written by an older install is replaced rather than duplicated.
func valueName(o Options) string { return o.ID }

// commandLine quotes the executable path — Program Files contains a space, and
// an unquoted path there launches the wrong thing.
func commandLine(o Options) string {
	parts := make([]string, 0, len(o.Args)+1)
	parts = append(parts, `"`+o.Exec+`"`)
	for _, a := range o.Args {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

func enable(o Options) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName(o), commandLine(o))
}

func disable(o Options) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(valueName(o)); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func enabled(o Options) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	switch _, _, err := k.GetStringValue(valueName(o)); {
	case err == nil:
		return true, nil
	case err == registry.ErrNotExist:
		return false, nil
	default:
		return false, err
	}
}
