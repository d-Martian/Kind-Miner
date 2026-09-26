package engine

import (
	"slices"
	"testing"
)

func TestScopeArgs(t *testing.T) {
	name, args := scopeArgs("kind-miner-xmrig-1-1", "CPUWeight=idle", "/opt/xmrig", []string{"--threads", "4"})
	if name != "systemd-run" {
		t.Fatalf("name = %q", name)
	}
	// --scope is what keeps the PID: systemd-run execs the miner in place
	// rather than handing it to the service manager to spawn.
	for _, want := range []string{"--user", "--scope", "--collect", "--unit=kind-miner-xmrig-1-1", "CPUWeight=idle"} {
		if !slices.Contains(args, want) {
			t.Errorf("args lack %q: %v", want, args)
		}
	}
	sep := slices.Index(args, "--")
	if sep < 0 || !slices.Equal(args[sep+1:], []string{"/opt/xmrig", "--threads", "4"}) {
		t.Errorf("the miner's own command line must follow --, untouched: %v", args)
	}
}

func TestScopeUnitNamesAreUnique(t *testing.T) {
	// systemd refuses a scope whose name is still loaded, and a restarted
	// miner can race the collection of the last one.
	if a, b := scopeUnitName(42), scopeUnitName(42); a == b {
		t.Errorf("two launches share the unit name %q", a)
	}
}

func TestUnifiedCgroup(t *testing.T) {
	// Fedora's hybrid layout lists a v1 net_cls line ahead of the unified one.
	const hybrid = "1:net_cls:/\n0::/user.slice/user-1000.slice/user@1000.service/app.slice/kind-miner-xmrig-7-1.scope\n"
	got, ok := unifiedCgroup(hybrid)
	if !ok || got != "/user.slice/user-1000.slice/user@1000.service/app.slice/kind-miner-xmrig-7-1.scope" {
		t.Errorf("got %q, %v; want the 0:: path", got, ok)
	}
	if _, ok := unifiedCgroup("1:net_cls:/\n"); ok {
		t.Error("a v1-only file reported a unified path")
	}
}

func TestCgroupIsIdle(t *testing.T) {
	cases := []struct {
		idle, weight string
		want         bool
	}{
		{"1\n", "1\n", true},    // CPUWeight=idle, as measured on systemd 259
		{"0\n", "1\n", true},    // CPUWeight=1 on an older systemd
		{"0\n", "100\n", false}, // accepted but not applied: no cpu controller
		{"", "", false},         // files missing
	}
	for _, c := range cases {
		if got := cgroupIsIdle(c.idle, c.weight); got != c.want {
			t.Errorf("cpu.idle=%q cpu.weight=%q: got %v, want %v", c.idle, c.weight, got, c.want)
		}
	}
}

func TestIoprioIdleValue(t *testing.T) {
	// IOPRIO_PRIO_VALUE(IOPRIO_CLASS_IDLE, 0) from linux/ioprio.h.
	if ioprioIdle != 0x6000 {
		t.Errorf("ioprioIdle = %#x, want 0x6000", ioprioIdle)
	}
}
