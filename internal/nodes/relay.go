package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// Relay makes an onion node look like a node on the loopback interface.
//
// p2pool reaches monerod two ways: HTTP RPC, which it can send through a SOCKS
// proxy, and ZMQ, which it cannot — libzmq has no SOCKS support. Pointed at an
// onion node, p2pool therefore gets RPC and never hears about a new block.
// The relay listens on a loopback address with the node's own ports and carries
// both over Tor, so p2pool just gets --host 127.0.0.2 and talks to it like a
// local node.
//
// p2pool keeps its own --socks5 for its peer connections. That does not catch
// the relay: p2pool sends RPC to any private address, the whole of 127.0.0.0/8
// included, without the proxy (is_private_address in p2pool's util.cpp, v4.18).
type Relay struct {
	remote Node
	local  Node
	dial   dialFunc

	listeners []net.Listener
	wg        sync.WaitGroup
	// ctx ends with Close, and every dial through Tor derives from it, so
	// closing never waits out a circuit that is still forming.
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closed  bool
	failing bool
}

// relayDialTimeout bounds one connection through Tor to the node. It matches
// the probe: a circuit that has not formed by then will not form soon.
const relayDialTimeout = torProbeTimeout

// RelayHost is the loopback address the relay for the index-th node listens
// on: 127.0.0.2 for the first, 127.0.0.3 for the next. 127.0.0.1 is left to
// everything else on the machine, which is what lets each relay keep its
// node's own ports without colliding with a local monerod.
func RelayHost(index int) string {
	return fmt.Sprintf("127.0.0.%d", 2+index)
}

// StartRelay starts relaying remote through Tor on RelayHost(index).
//
// Where that address cannot be bound — macOS configures only 127.0.0.1 on its
// loopback interface — the relay falls back to 127.0.0.1 on ports the system
// picks. Local reports where it actually listens, which is what p2pool must be
// given.
func StartRelay(remote Node, index int) (*Relay, error) {
	dial, err := torDialer(remote.Host)
	if err != nil {
		return nil, err
	}
	return startRelay(remote, RelayHost(index), dial)
}

func startRelay(remote Node, host string, dial dialFunc) (*Relay, error) {
	r := &Relay{remote: remote, dial: dial, conns: make(map[net.Conn]struct{})}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	rpc, zmq, err := listenPair(host, remote.RPCPort, remote.ZMQPort)
	if err != nil {
		fallback, ferr := listenPairAnyPort("127.0.0.1")
		if ferr != nil {
			r.cancel()
			return nil, fmt.Errorf("relay for %s: %v; fallback: %w", remote.Addr(), err, ferr)
		}
		log.Printf("tor relay: cannot listen on %s (%v); using 127.0.0.1 on free ports", host, err)
		rpc, zmq = fallback[0], fallback[1]
	}
	r.listeners = []net.Listener{rpc, zmq}
	r.local = Node{
		Host:    rpc.Addr().(*net.TCPAddr).IP.String(),
		RPCPort: rpc.Addr().(*net.TCPAddr).Port,
		ZMQPort: zmq.Addr().(*net.TCPAddr).Port,
	}
	r.serve(rpc, remote.RPCPort, r.forward)
	r.serve(zmq, remote.ZMQPort, r.forwardZMQ)
	log.Printf("tor relay: %s → %s (RPC), %s → ZMQ %d",
		r.local.Addr(), remote.Addr(), net.JoinHostPort(r.local.Host, strconv.Itoa(r.local.ZMQPort)), remote.ZMQPort)
	return r, nil
}

func listenPair(host string, rpcPort, zmqPort int) (net.Listener, net.Listener, error) {
	rpc, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(rpcPort)))
	if err != nil {
		return nil, nil, err
	}
	zmq, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(zmqPort)))
	if err != nil {
		rpc.Close()
		return nil, nil, err
	}
	return rpc, zmq, nil
}

func listenPairAnyPort(host string) ([2]net.Listener, error) {
	rpc, zmq, err := listenPair(host, 0, 0)
	return [2]net.Listener{rpc, zmq}, err
}

// Local is the loopback node p2pool should follow instead of the onion.
func (r *Relay) Local() Node { return r.local }

func (r *Relay) serve(l net.Listener, remotePort int, forward func(net.Conn, string)) {
	target := net.JoinHostPort(r.remote.Host, strconv.Itoa(remotePort))
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return // closed
			}
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				forward(c, target)
			}()
		}
	}()
}

// forward carries one connection to the node and back. A failed dial just
// closes the local side: p2pool retries RPC on its own and reconnects ZMQ,
// which is the same thing it would do against a node that went away.
func (r *Relay) forward(local net.Conn, target string) {
	if !r.track(local) {
		local.Close()
		return
	}
	defer r.untrack(local)

	ctx, cancel := context.WithTimeout(r.ctx, relayDialTimeout)
	remote, err := r.dial(ctx, "tcp", target)
	cancel()
	r.noteDial(err)
	if err != nil {
		local.Close()
		return
	}
	if !r.track(remote) {
		remote.Close()
		local.Close()
		return
	}
	defer r.untrack(remote)
	splice(local, remote)
}

// localHandshakeTimeout bounds p2pool's half of a ZMQ handshake, which runs
// over loopback in milliseconds; it only stops a stuck client holding a
// connection open.
const localHandshakeTimeout = 10 * time.Second

// forwardZMQ carries one ZMQ connection, handshaking with each side itself
// (see zmtp.go): with p2pool at once, inside the one second p2pool allows,
// and with the node through Tor, however long that takes. Until the node's
// side is ready, what p2pool sends — its subscriptions — waits in the socket.
func (r *Relay) forwardZMQ(local net.Conn, target string) {
	if !r.track(local) {
		local.Close()
		return
	}
	defer r.untrack(local)

	type dialed struct {
		conn net.Conn
		err  error
	}
	node := make(chan dialed, 1)
	ctx, cancel := context.WithTimeout(r.ctx, relayDialTimeout)
	defer cancel()
	go func() {
		remote, err := r.dial(ctx, "tcp", target)
		if err == nil && !r.track(remote) {
			// Tracked from the start, so Close cuts a handshake still
			// waiting on Tor instead of waiting out the dial timeout.
			remote.Close()
			remote, err = nil, errRelayClosed
		}
		if err == nil {
			// Giving up on the local side cancels ctx; that has to reach a
			// read already blocked on the node.
			stop := context.AfterFunc(ctx, func() { remote.SetDeadline(time.Now()) })
			if d, ok := ctx.Deadline(); ok {
				remote.SetDeadline(d)
			}
			err = zmtpHandshake(remote, "SUB")
			stop()
			if err != nil {
				r.untrack(remote)
				remote.Close()
				remote = nil
			} else {
				remote.SetDeadline(time.Time{})
			}
		}
		node <- dialed{remote, err}
	}()

	local.SetDeadline(time.Now().Add(localHandshakeTimeout))
	err := zmtpHandshake(local, "PUB")
	local.SetDeadline(time.Time{})
	if err != nil {
		// Not p2pool, or it gave up, or the relay is closing: nothing to
		// carry. Stop the node's side rather than wait for it.
		local.Close()
		cancel()
		if d := <-node; d.conn != nil {
			r.untrack(d.conn)
			d.conn.Close()
		}
		return
	}

	d := <-node
	if !errors.Is(d.err, errRelayClosed) {
		r.noteDial(d.err)
	}
	if d.err != nil {
		local.Close() // p2pool reconnects, as it would to a node that went away
		return
	}
	defer r.untrack(d.conn)
	splice(local, d.conn)
}

// errRelayClosed is a connection the relay refused because it is closing.
var errRelayClosed = errors.New("tor relay: closed")

// splice copies both ways; when either side finishes, it closes both so the
// other copy unblocks. There is deliberately no idle timeout: a ZMQ
// subscription is quiet for minutes between blocks, and cutting it would
// lose the next block notification.
func splice(local, remote net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go pipe(remote, local)
	go pipe(local, remote)
	<-done
	local.Close()
	remote.Close()
	<-done
}

// noteDial logs when reaching the node through Tor starts or stops failing,
// not on every attempt: p2pool polls RPC every few seconds, and a Tor outage
// would otherwise bury the log.
func (r *Relay) noteDial(err error) {
	r.mu.Lock()
	was := r.failing
	r.failing = err != nil
	r.mu.Unlock()
	switch {
	case err != nil && !was:
		log.Printf("tor relay: cannot reach %s through Tor: %v", r.remote.Host, err)
	case err == nil && was:
		log.Printf("tor relay: reaching %s through Tor again", r.remote.Host)
	}
}

func (r *Relay) track(c net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.conns[c] = struct{}{}
	return true
}

func (r *Relay) untrack(c net.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

// Close stops listening, drops every relayed connection, and waits for the
// relay's goroutines to finish.
func (r *Relay) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.cancel()
	var errs []error
	for _, l := range r.listeners {
		errs = append(errs, l.Close())
	}
	for c := range r.conns {
		c.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
	return errors.Join(errs...)
}
