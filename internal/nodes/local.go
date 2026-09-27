package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A Monero node on this machine beats any remote one: templates arrive with no
// network in between, nothing about the miner leaves the machine, and there is
// no Tor to start or relay to run. So when the user has not named a node,
// kind-miner looks for one here first — but only uses it if p2pool can: synced,
// answering RPC, and publishing ZMQ, which monerod only does when started with
// --zmq-pub. A node that is running but not usable is named in the log with the
// one change that would make it usable, and mining falls back to the remote
// node rather than waiting.

// Where a local monerod listens by default, and where p2pool setups
// conventionally publish ZMQ.
const (
	localRPCAddr = "127.0.0.1:18081"
	localZMQAddr = "127.0.0.1:18083"
)

// localProbeTimeout bounds each check. A local port answers in milliseconds or
// refuses at once; anything slower is not worth delaying startup for.
const localProbeTimeout = 2 * time.Second

// LocalState is what the probe found on this machine.
type LocalState int

const (
	LocalAbsent  LocalState = iota // nothing listening: the common case
	LocalUsable                    // synced, RPC and ZMQ both answer
	LocalSyncing                   // running, still catching up
	LocalNoZMQ                     // running and synced, but no ZMQ publisher
	LocalLocked                    // RPC wants a login kind-miner does not have
	LocalBroken                    // answered, but not like monerod
)

// LocalNode is the result of looking for a Monero node on this machine.
type LocalNode struct {
	State LocalState
	Node  Node
	// Detail completes the log line for the states that are not usable.
	Detail string
}

// ProbeLocal looks for a usable monerod on this machine.
func ProbeLocal() LocalNode {
	return probeLocalAt(localRPCAddr, localZMQAddr, localProbeTimeout)
}

func probeLocalAt(rpcAddr, zmqAddr string, timeout time.Duration) LocalNode {
	info, err := localInfo(rpcAddr, timeout)
	var zmqErr error
	if err == nil {
		zmqErr = probeZMTP(context.Background(), directDial, zmqAddr, timeout)
	}
	return judgeLocal(rpcAddr, zmqAddr, info, err, zmqErr)
}

// monerodInfo is the part of get_info the verdict needs.
type monerodInfo struct {
	Status       string `json:"status"`
	Synchronized bool   `json:"synchronized"`
	BusySyncing  bool   `json:"busy_syncing"`
	Height       uint64 `json:"height"`
	TargetHeight uint64 `json:"target_height"`
}

// errRPCLogin marks a 401: monerod is there, behind --rpc-login.
var errRPCLogin = errors.New("RPC login required")

func localInfo(rpcAddr string, timeout time.Duration) (monerodInfo, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post("http://"+rpcAddr+"/json_rpc", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":"0","method":"get_info"}`))
	if err != nil {
		return monerodInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return monerodInfo{}, errRPCLogin
	}
	var body struct {
		Result monerodInfo `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return monerodInfo{}, fmt.Errorf("not a monerod reply: %w", err)
	}
	if body.Result.Status != "OK" {
		return monerodInfo{}, fmt.Errorf("get_info status %q", body.Result.Status)
	}
	return body.Result, nil
}

// judgeLocal turns what the probe saw into a verdict. It is separate so every
// case can be tested without a monerod.
func judgeLocal(rpcAddr, zmqAddr string, info monerodInfo, rpcErr, zmqErr error) LocalNode {
	switch {
	case rpcErr != nil && isNothingListening(rpcErr):
		return LocalNode{State: LocalAbsent}
	case errors.Is(rpcErr, errRPCLogin):
		return LocalNode{State: LocalLocked, Detail: fmt.Sprintf(
			"a Monero node is running here (%s), but its RPC needs a login kind-miner does not have", rpcAddr)}
	case rpcErr != nil:
		return LocalNode{State: LocalBroken, Detail: fmt.Sprintf(
			"something answers on %s, but not as a usable Monero node (%v)", rpcAddr, rpcErr)}
	case !info.Synchronized || info.BusySyncing:
		progress := ""
		if info.TargetHeight > 0 && info.Height < info.TargetHeight {
			progress = fmt.Sprintf(", at %.1f%% (block %d of %d)",
				100*float64(info.Height)/float64(info.TargetHeight), info.Height, info.TargetHeight)
		}
		return LocalNode{State: LocalSyncing, Detail: fmt.Sprintf(
			"the Monero node running here is still syncing%s; kind-miner will switch to it when it has finished", progress)}
	case zmqErr != nil:
		return LocalNode{State: LocalNoZMQ, Detail: fmt.Sprintf(
			"the Monero node running here has no ZMQ, which p2pool needs; restart monerod with --zmq-pub tcp://%s and kind-miner will switch to it", zmqAddr)}
	}
	return LocalNode{State: LocalUsable, Node: nodeAt(rpcAddr, zmqAddr)}
}

func nodeAt(rpcAddr, zmqAddr string) Node {
	host, rpcPort, _ := net.SplitHostPort(rpcAddr)
	_, zmqPort, _ := net.SplitHostPort(zmqAddr)
	rpc, _ := strconv.Atoi(rpcPort)
	zmq, _ := strconv.Atoi(zmqPort)
	return Node{Host: host, RPCPort: rpc, ZMQPort: zmq}
}

// isNothingListening reports a failure to connect at all, which is how a
// machine with no node answers — refused on Linux and macOS, the WSA
// equivalent on Windows. Matching the dial step rather than one errno keeps
// that quiet everywhere; a reply that fails later means something is there.
func isNothingListening(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}
