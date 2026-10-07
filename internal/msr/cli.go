package msr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Where the live command keeps things.
const (
	devDir    = "/dev/cpu"
	statePath = "/var/lib/kind-miner-msr/original.json"
	unitPath  = "/etc/systemd/system/kind-miner-msr.service"
	unitName  = "kind-miner-msr.service"
)

// Warning is said every time the boost is turned on, and in the app: the
// registers are the whole machine's, not the miner's.
const Warning = "This turns off CPU prefetchers for the whole machine until reboot, so other programs may run a little slower while it is on. It suits a machine left mining, not one you work on."

// Run is `kind-miner msr on [--at-boot] | off | status`, for both binaries.
func Run(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: msr on [--at-boot] | off | status")
	}
	if os.Getenv("FLATPAK_ID") != "" || exists("/.flatpak-info") {
		return errors.New("the Flatpak cannot reach the CPU's registers; run this once from the AppImage or the tarball instead: sudo ./kind-miner msr on")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "386" {
		return errors.New("the MSR boost is for x86 CPUs (Intel and AMD); this machine has none to set")
	}
	cpuinfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return err
	}
	p, ok := Detect(string(cpuinfo))
	if !ok {
		return errors.New("xmrig has no MSR preset for this CPU, so there is nothing to set")
	}
	self := selfPath()
	root := os.Geteuid() == 0
	// Before anything that asks for root: under lockdown no one can set the
	// registers, and telling someone to sudo only to refuse them after is
	// worse than saying so now. Gupax's xmrig is refused the same way here.
	if err := checkLockdown(); err != nil {
		if args[0] == "status" {
			fmt.Fprintf(w, "MSR boost: not possible here — %v.\n", err)
			return nil
		}
		return err
	}

	switch args[0] {
	case "status":
		if !root {
			if exists(statePath) {
				fmt.Fprintf(w, "MSR boost: on (%s preset). Run with sudo to check the registers themselves.\n", p.Name)
			} else {
				fmt.Fprintln(w, "MSR boost: off.")
			}
			return nil
		}
		if err := loadDriver(); err != nil {
			return err
		}
		on, err := Applied(Device{Dir: devDir}, p)
		if err != nil {
			return err
		}
		state := "off"
		if on {
			state = "on"
		}
		boot := ""
		if exists(unitPath) {
			boot = ", and set again at every boot and resume"
		}
		fmt.Fprintf(w, "MSR boost: %s (%s preset%s).\n", state, p.Name, boot)
		return nil

	case "on":
		if !root {
			return fmt.Errorf("the registers can only be set as root:\n\n  sudo %s msr on%s", self, strings.Join(args[1:], " "))
		}
		if err := loadDriver(); err != nil {
			return err
		}
		if err := Apply(Device{Dir: devDir}, p, statePath); err != nil {
			return err
		}
		fmt.Fprintf(w, "MSR boost on (%s preset). xmrig picks it up at once; no restart needed.\n%s\n", p.Name, Warning)
		if len(args) > 1 && args[1] == "--at-boot" {
			if err := installUnit(self); err != nil {
				return fmt.Errorf("the boost is on, but setting it up for every boot failed: %w", err)
			}
			fmt.Fprintln(w, "It will be set again at every boot and after every resume. Undo all of it with: sudo "+self+" msr off")
		} else {
			fmt.Fprintln(w, "It lasts until the next reboot or sleep. Make it stick with: sudo "+self+" msr on --at-boot")
		}
		return nil

	case "off":
		if !root {
			return fmt.Errorf("the registers can only be set as root:\n\n  sudo %s msr off", self)
		}
		removeUnit()
		if err := loadDriver(); err != nil {
			return err
		}
		if err := Restore(Device{Dir: devDir}, statePath); err != nil {
			return err
		}
		fmt.Fprintln(w, "MSR boost off: the registers are back as they were.")
		return nil
	}
	return errors.New("usage: msr on [--at-boot] | off | status")
}

// selfPath is the command to tell the user to run, and what a boot unit runs:
// the AppImage file itself rather than its temporary mount.
func selfPath() string {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p
	}
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "kind-miner"
}

// Blocked says why the boost cannot be used on this machine, or nil: for the
// app, which offers the command only where it can work.
func Blocked() error { return checkLockdown() }

// checkLockdown refuses early under kernel lockdown (Secure Boot on most
// distributions), which forbids writing MSRs: the write would fail per CPU
// with a bare "operation not permitted".
func checkLockdown() error {
	data, err := os.ReadFile("/sys/kernel/security/lockdown")
	if err != nil {
		return nil // no lockdown support: nothing in the way
	}
	if strings.Contains(string(data), "[integrity]") || strings.Contains(string(data), "[confidentiality]") {
		return errors.New("the kernel is in lockdown mode, usually because Secure Boot is on, and lockdown forbids setting CPU registers for anyone — root, kind-miner or another miner")
	}
	return nil
}

// loadDriver makes sure /dev/cpu/N/msr exists.
func loadDriver() error {
	if exists(devDir + "/0/msr") {
		return nil
	}
	if out, err := exec.Command("modprobe", "msr").CombinedOutput(); err != nil {
		return fmt.Errorf("loading the msr kernel module: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installUnit(self string) error {
	unit := fmt.Sprintf(`# Written by "kind-miner msr on --at-boot"; removed by "msr off".
# Sets xmrig's MSR preset at boot and after resume, which reset the registers.
[Unit]
Description=kind-miner MSR boost for RandomX
After=suspend.target hibernate.target hybrid-sleep.target

[Service]
Type=oneshot
ExecStart=%q msr on

[Install]
WantedBy=multi-user.target suspend.target hibernate.target hybrid-sleep.target
`, self)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", unitName).CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, out)
	}
	return nil
}

func removeUnit() {
	if !exists(unitPath) {
		return
	}
	_ = exec.Command("systemctl", "disable", unitName).Run()
	_ = os.Remove(unitPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
