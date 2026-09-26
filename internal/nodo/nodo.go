// Package nodo recognises a MoneroNodo and reads the node settings p2pool
// needs from it.
//
// A Nodo keeps its whole configuration in one world-readable JSON file that its
// own UIs (the touchscreen, the SSH TUI, the web page) edit and its start
// scripts read. Kind Miner only ever reads it. /home/nodo belongs to Nodo's
// weekly updater, and anything written there is either overwritten or, worse,
// read back by Nodo's scripts as if the owner had set it.
package nodo

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ConfigPath is where every Nodo keeps its settings.
const ConfigPath = "/home/nodo/variables/config.json"

// RPCPort is monerod's default, unrestricted RPC listener. Nodo's start script
// adds a restricted listener on monero_rpc_port for the outside world but
// never moves or removes this one, and it is the one p2pool needs: with
// --no-randomx, p2pool checks share PoW through monerod's calc_pow, which the
// restricted RPC refuses.
const RPCPort = 18081

// Node is what p2pool needs from a Nodo's configuration.
type Node struct {
	// ZMQPort is monerod's ZMQ publisher (zmq_pub).
	ZMQPort int
	// RPCLogin is "user:password" when the owner has turned RPC login on, and
	// empty otherwise. Nodo passes the same --rpc-login to monerod whichever
	// listener is asked, so p2pool must present it on the local one too.
	RPCLogin string
	// AnonRPC moves monerod's listeners from every interface to loopback
	// only. p2pool connects over loopback either way, but Nodo restarts
	// monerod when it changes, so it is tracked with the rest.
	AnonRPC bool
}

// Detect reports whether this machine is a Nodo and, if so, its node settings.
// A missing file means "not a Nodo"; a present but unreadable or malformed one
// is also treated as not a Nodo, with the reason, so the caller can say why the
// profile was not applied instead of connecting with guessed ports.
func Detect() (Node, bool, error) { return detectAt(ConfigPath) }

func detectAt(path string) (Node, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Node{}, false, nil
		}
		return Node{}, false, err
	}
	n, err := parse(data)
	if err != nil {
		return Node{}, false, err
	}
	return n, true, nil
}

// parse reads the fields Kind Miner uses out of Nodo's config.json.
//
// Nodo's own tools are loose with types — booleans are the strings "TRUE" and
// "FALSE", and a port written by one UI can be a number where another writes a
// string — so each field is read the way Nodo's shell scripts read it (jq -r),
// not by Go's strict decoding.
func parse(data []byte) (Node, error) {
	var file struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return Node{}, fmt.Errorf("nodo config: %w", err)
	}
	if file.Config == nil {
		return Node{}, fmt.Errorf("nodo config: no \"config\" object")
	}
	c := file.Config

	zmq, err := strconv.Atoi(scalar(c["zmq_pub"]))
	if err != nil || zmq < 1 || zmq > 65535 {
		return Node{}, fmt.Errorf("nodo config: zmq_pub %q is not a port", scalar(c["zmq_pub"]))
	}
	n := Node{ZMQPort: zmq, AnonRPC: truthy(c["anon_rpc"])}
	// Mirrors monerod.sh: the login applies only with rpc_enabled, and only
	// when a username is set.
	if user := scalar(c["rpcu"]); truthy(c["rpc_enabled"]) && user != "" {
		n.RPCLogin = user + ":" + scalar(c["rpcp"])
	}
	return n, nil
}

// scalar renders a JSON value the way jq -r does: strings unquoted, numbers
// and booleans as written, anything absent or null as "".
func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	v := strings.TrimSpace(string(raw))
	if v == "null" {
		return ""
	}
	return v
}

// truthy matches Nodo's own test, `[ "$X" == "TRUE" ]`, and also accepts a JSON
// true, which some Nodo fields (night_mode) already use.
func truthy(raw json.RawMessage) bool {
	return scalar(raw) == "TRUE" || scalar(raw) == "true"
}
