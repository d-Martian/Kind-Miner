package nodo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stockConfig is the config.json a Nodo ships with (MoneroNodo/Nodo,
// home/nodo/variables/config.json), trimmed to the fields around the ones read.
const stockConfig = `{
	"config": {
		"monero_public_port": 18081,
		"monero_rpc_port": 18089,
		"zmq_pub": 18083,
		"anon_rpc": "FALSE",
		"rpc_enabled": "FALSE",
		"rpcp": "password",
		"rpcu": "nodo",
		"night_mode": false
	}
}`

func withConfig(fields string) string {
	return strings.Replace(stockConfig, `"night_mode": false`, fields+`,
		"night_mode": false`, 1)
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Node
		wantErr bool
	}{
		{
			// The stock file carries a username and password, but monerod.sh
			// passes --rpc-login only when rpc_enabled is TRUE.
			name: "a stock Nodo has no RPC login",
			in:   stockConfig,
			want: Node{ZMQPort: 18083},
		},
		{
			name: "turning RPC on in Nodo's UI makes the login count",
			in:   strings.Replace(stockConfig, `"rpc_enabled": "FALSE"`, `"rpc_enabled": "TRUE"`, 1),
			want: Node{ZMQPort: 18083, RPCLogin: "nodo:password"},
		},
		{
			name: "RPC on with no username sets no login, as in monerod.sh",
			in: strings.NewReplacer(`"rpc_enabled": "FALSE"`, `"rpc_enabled": "TRUE"`,
				`"rpcu": "nodo"`, `"rpcu": ""`).Replace(stockConfig),
			want: Node{ZMQPort: 18083},
		},
		{
			name: "anon RPC is read from Nodo's TRUE/FALSE strings",
			in:   strings.Replace(stockConfig, `"anon_rpc": "FALSE"`, `"anon_rpc": "TRUE"`, 1),
			want: Node{ZMQPort: 18083, AnonRPC: true},
		},
		{
			name: "a port some UI wrote as a string still reads",
			in:   strings.Replace(stockConfig, `"zmq_pub": 18083`, `"zmq_pub": "28083"`, 1),
			want: Node{ZMQPort: 28083},
		},
		{
			name:    "a missing ZMQ port is an error, not a guess",
			in:      strings.Replace(stockConfig, `"zmq_pub": 18083,`, ``, 1),
			wantErr: true,
		},
		{
			// What a reader sees mid-way through the SSH UI's truncate-and-write.
			name:    "a half-written file is an error",
			in:      stockConfig[:len(stockConfig)/2],
			wantErr: true,
		},
		{name: "an empty file is an error", in: "", wantErr: true},
		{name: "JSON without the config object is an error", in: `{"other": {}}`, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse([]byte(c.in))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestDetectAt(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := detectAt(filepath.Join(dir, "config.json")); ok || err != nil {
		t.Errorf("no file: ok=%v err=%v, want not a Nodo and no error", ok, err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := detectAt(path); ok || err == nil {
		t.Errorf("malformed file: ok=%v err=%v, want not a Nodo, with the reason", ok, err)
	}
}

// followHarness runs follow against a temp config.json with a short settle, and
// collects what it reports.
func followHarness(t *testing.T, initial string) (path string, poke func(content string), got chan Node) {
	t.Helper()
	old := settle
	settle = 20 * time.Millisecond
	t.Cleanup(func() { settle = old })

	path = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	last, _ := parse([]byte(initial))
	events := make(chan struct{}, 1)
	got = make(chan Node, 8)
	go follow(path, events, last, func(n Node) { got <- n })
	t.Cleanup(func() { close(events) })

	poke = func(content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		events <- struct{}{}
	}
	return path, poke, got
}

func TestFollowReportsAChange(t *testing.T) {
	_, poke, got := followHarness(t, stockConfig)
	poke(strings.Replace(stockConfig, `"zmq_pub": 18083`, `"zmq_pub": 18093`, 1))
	select {
	case n := <-got:
		if n.ZMQPort != 18093 {
			t.Errorf("reported %+v, want ZMQ 18093", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a changed ZMQ port was never reported")
	}
}

func TestFollowIgnoresUnrelatedSaves(t *testing.T) {
	_, poke, got := followHarness(t, stockConfig)
	// The owner changing the display currency must not restart p2pool.
	poke(withConfig(`"ticker": "eur"`))
	select {
	case n := <-got:
		t.Errorf("reported %+v for a save that changed nothing p2pool uses", n)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestFollowSkipsAHalfWrittenFile(t *testing.T) {
	_, poke, got := followHarness(t, stockConfig)
	changed := strings.Replace(stockConfig, `"rpc_enabled": "FALSE"`, `"rpc_enabled": "TRUE"`, 1)
	poke(changed[:len(changed)/2])
	select {
	case n := <-got:
		t.Fatalf("acted on a half-written file: %+v", n)
	case <-time.After(200 * time.Millisecond):
	}
	poke(changed)
	select {
	case n := <-got:
		if n.RPCLogin != "nodo:password" {
			t.Errorf("reported %+v once the write finished, want the new login", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the finished write was never reported")
	}
}
