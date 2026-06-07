package gui

import (
	"log"
	"os"
	"os/exec"
	"runtime"
)

// openInEditor opens path in the user's preferred editor (used as an advanced
// escape hatch for editing the config file directly). Inside a Flatpak sandbox
// it uses flatpak-spawn --host to reach the host's editor.
func openInEditor(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad", path)
	case "darwin":
		cmd = exec.Command("open", "-t", path)
	default:
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = "xdg-open"
		}
		if _, err := os.Stat("/.flatpak-info"); err == nil {
			cmd = exec.Command("flatpak-spawn", "--host", editor, path)
		} else {
			cmd = exec.Command(editor, path)
		}
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open editor: %v", err)
	}
}

// openFolder opens a directory in the system file manager.
func openFolder(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		if _, err := os.Stat("/.flatpak-info"); err == nil {
			cmd = exec.Command("flatpak-spawn", "--host", "xdg-open", path)
		} else {
			cmd = exec.Command("xdg-open", path)
		}
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open folder: %v", err)
	}
}
