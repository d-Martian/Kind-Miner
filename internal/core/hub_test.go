package core

import (
	"os"
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/hub"
)

func TestHubServing(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"off by default", *config.Defaults(), false},
		{"on with our own p2pool", config.Config{Mode: config.ModeP2PoolLocal, ManageP2Pool: true, Hub: config.HubOptions{Serve: true}}, true},
		{"not without our own p2pool", config.Config{Mode: config.ModeP2PoolLocal, Hub: config.HubOptions{Serve: true}}, false},
		{"never on a machine mining to a hub", config.Config{Mode: config.ModeHub, ManageP2Pool: true, Hub: config.HubOptions{Serve: true}}, false},
	}
	for _, tt := range tests {
		if got := hubServing(&tt.cfg); got != tt.want {
			t.Errorf("%s: %v", tt.name, got)
		}
	}
}

func TestWorkerName(t *testing.T) {
	if got := workerName(&config.Config{WorkerName: "Dad's laptop"}); got != "Dad-s-laptop" {
		t.Errorf("set name: %q", got)
	}
	host, _ := os.Hostname()
	want := hub.WorkerName(host)
	if want == "" {
		want = "desktop"
	}
	if got := workerName(&config.Config{}); got != want {
		t.Errorf("blank name: %q, want the hostname's %q", got, want)
	}
}
