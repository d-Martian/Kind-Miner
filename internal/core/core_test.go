package core

import (
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
)

func TestTorNeeded(t *testing.T) {
	cases := []struct {
		name   string
		mode   config.Mode
		remote string
		want   bool
	}{
		{"remote default Nodo is .onion", config.ModeP2PoolRemote, "", true},
		{"remote custom .onion node", config.ModeP2PoolRemote, "abcdef.onion:18089", true},
		{"remote custom clearnet node", config.ModeP2PoolRemote, "192.168.8.192:18089", false},
		{"local monerod", config.ModeP2PoolLocal, "", false},
		{"traditional pool", config.ModePool, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(&config.Config{Mode: tc.mode, RemoteNode: tc.remote})
			if got := s.torNeeded(); got != tc.want {
				t.Errorf("torNeeded() = %v, want %v", got, tc.want)
			}
		})
	}
}
