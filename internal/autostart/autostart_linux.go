//go:build linux || freebsd || openbsd || netbsd

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// autostartDir is a variable so tests can redirect it to a temp directory.
var autostartDir = func() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "autostart"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart"), nil
}

// inFlatpak reports whether we are inside a Flatpak sandbox, where writing to
// the host's autostart directory is not possible (and a write to the sandboxed
// path would be invisible to the session).
var inFlatpak = func() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

func desktopPath(o Options) (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, o.ID+".desktop"), nil
}

// execLine renders Exec= per the desktop entry spec: reserved characters mean a
// path with a space would otherwise be read as a command plus arguments.
func execLine(exec string, args []string) string {
	quote := func(s string) string {
		if !strings.ContainsAny(s, ` \t"'\\><~|&;$*?#()`+"`") {
			return s
		}
		r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`)
		return `"` + r.Replace(s) + `"`
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quote(exec))
	for _, a := range args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func desktopEntry(o Options) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=Mine Monero with spare CPU cycles
Exec=%s
Icon=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, o.Name, execLine(o.Exec, o.Args), o.ID)
}

func enable(o Options) error {
	if inFlatpak() {
		return portalAutostart(o, true)
	}
	path, err := desktopPath(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(desktopEntry(o)), 0o644)
}

func disable(o Options) error {
	if inFlatpak() {
		return portalAutostart(o, false)
	}
	path, err := desktopPath(o)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func enabled(o Options) (bool, error) {
	if inFlatpak() {
		// The background portal can request autostart but has no method to read
		// it back; the caller's stored preference is the source of truth.
		return false, ErrStateUnknown
	}
	path, err := desktopPath(o)
	if err != nil {
		return false, err
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}
