package hub

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"sort"
	"strconv"
	"time"
)

// A desktop finds hubs by asking the LAN: one UDP broadcast, and every hub
// that hears it answers with who it is. It is what lets a Nodo owner set their
// hub up without knowing its address, let alone SSH.
//
// The beacon is unauthenticated, and needn't be: it only says where to
// connect. The certificate it announces is checked when the desktop connects,
// and pinned from then on, so a beacon lying about it gets the liar pinned
// only if it also answers the connection — which is the trust-on-first-use
// Probe describes, not something the beacon adds.

// probe is what a desktop broadcasts. It is padded to be larger than any
// answer, so the beacon can never be used to send a spoofed victim more than
// it was sent.
var probe = append([]byte("kind-miner-discover/1\n"), make([]byte, 512)...)

// Beacon is a hub's answer to the probe.
type Beacon struct {
	Hello
	APIPort     int    `json:"api_port"`
	Fingerprint string `json:"fingerprint"`
}

// maxBeacon bounds an answer; one is a couple of hundred bytes.
const maxBeacon = 512

// Found is a hub that answered.
type Found struct {
	Hello
	Host        string
	APIPort     int
	Fingerprint [32]byte
}

// APIAddr is where to reach the hub's API.
func (f Found) APIAddr() string { return net.JoinHostPort(f.Host, strconv.Itoa(f.APIPort)) }

// Announcer answers probes until closed.
type Announcer struct {
	conn net.PacketConn
}

// Announce answers probes arriving on UDP port with beacon(). The port is
// always DefaultAPIPort in practice — a desktop cannot know a hub moved its
// API elsewhere before it has found the hub — and the beacon says where the
// API really is.
func Announce(port int, beacon func() Beacon) (*Announcer, error) {
	conn, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	a := &Announcer{conn: conn}
	go a.serve(beacon)
	return a, nil
}

func (a *Announcer) serve(beacon func() Beacon) {
	buf := make([]byte, len(probe)+1)
	for {
		n, from, err := a.conn.ReadFrom(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("hub beacon: %v", err)
			}
			return
		}
		if n != len(probe) || string(buf[:n]) != string(probe) {
			continue
		}
		data, err := json.Marshal(beacon())
		if err != nil || len(data) > maxBeacon {
			continue
		}
		_, _ = a.conn.WriteTo(data, from)
	}
}

// Close stops answering.
func (a *Announcer) Close() { _ = a.conn.Close() }

// Discover broadcasts the probe on every IPv4 network this machine is on and
// returns the hubs that answer within ctx, in a stable order.
func Discover(ctx context.Context) ([]Found, error) {
	return discover(ctx, broadcastAddrs(DefaultAPIPort))
}

// discoverWait is how long a search listens when ctx gives no deadline. A hub
// on the LAN answers in a millisecond or two; a Nodo busy verifying a block
// is given a lot longer than that.
const discoverWait = 1500 * time.Millisecond

func discover(ctx context.Context, targets []*net.UDPAddr) ([]Found, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, discoverWait)
		defer cancel()
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	go func() {
		<-ctx.Done()
		_ = conn.SetDeadline(time.Now())
	}()

	sent := 0
	for _, t := range targets {
		if _, err := conn.WriteToUDP(probe, t); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return nil, errors.New("this computer is not on a network")
	}

	seen := map[string]Found{}
	buf := make([]byte, maxBeacon+1)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // the deadline: the search is over
		}
		var b Beacon
		if n > maxBeacon || json.Unmarshal(buf[:n], &b) != nil || b.APIPort <= 0 || b.APIPort > 65535 {
			continue
		}
		fp, err := hex.DecodeString(b.Fingerprint)
		if err != nil || len(fp) != 32 {
			continue
		}
		f := Found{Hello: b.Hello, Host: from.IP.String(), APIPort: b.APIPort}
		copy(f.Fingerprint[:], fp)
		// One hub heard on two broadcasts answers twice.
		seen[f.Host+"/"+b.Fingerprint] = f
	}
	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Host < out[j].Host
	})
	return out, nil
}

// broadcastAddrs are where to send the probe: the limited broadcast, which
// some systems send out of one interface only, and each network's own
// broadcast address, which reaches the rest.
func broadcastAddrs(port int) []*net.UDPAddr {
	out := []*net.UDPAddr{{IP: net.IPv4bcast, Port: port}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, mask := n.IP.To4(), net.IP(n.Mask).To4()
			if mask == nil {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip[i] | ^mask[i]
			}
			out = append(out, &net.UDPAddr{IP: b, Port: port})
		}
	}
	return out
}
