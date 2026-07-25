package autostart

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// launchAgentsDir is a variable so tests can redirect it to a temp directory.
var launchAgentsDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func plistPath(o Options) (string, error) {
	dir, err := launchAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, o.ID+".plist"), nil
}

func escapePlist(s string) string {
	var out strings.Builder
	if err := xml.EscapeText(&out, []byte(s)); err != nil {
		return s
	}
	return out.String()
}

// plistEntry renders a LaunchAgent that runs the app once at login.
//
// launchd is not asked to keep the process alive: kind-miner is a tray app the
// user is free to quit, and KeepAlive would resurrect it against their wishes.
// No `launchctl bootstrap` call is made either — writing the file is enough from
// the next login onwards, and bootstrapping into the correct GUI domain is
// fragile across macOS versions.
func plistEntry(o Options) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + escapePlist(o.ID) + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{o.Exec}, o.Args...) {
		b.WriteString("\t\t<string>" + escapePlist(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

func enable(o Options) error {
	path, err := plistPath(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, plistEntry(o), 0o644); err != nil {
		return fmt.Errorf("write LaunchAgent: %w", err)
	}
	return nil
}

func disable(o Options) error {
	path, err := plistPath(o)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func enabled(o Options) (bool, error) {
	path, err := plistPath(o)
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
