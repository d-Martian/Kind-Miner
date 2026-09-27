package nodes

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJudgeLocal(t *testing.T) {
	synced := monerodInfo{Status: "OK", Synchronized: true, Height: 3_400_000, TargetHeight: 3_400_000}
	dialErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	cases := []struct {
		name       string
		info       monerodInfo
		rpcErr     error
		zmqErr     error
		want       LocalState
		wantDetail string
	}{
		{name: "nothing listening is the quiet common case", rpcErr: dialErr, want: LocalAbsent},
		{name: "synced with ZMQ is used", info: synced, want: LocalUsable},
		{
			name: "still syncing says how far and what to do",
			info: monerodInfo{Status: "OK", Height: 1_700_000, TargetHeight: 3_400_000},
			want: LocalSyncing, wantDetail: "50.0% (block 1700000 of 3400000)",
		},
		{
			// monerod keeps reporting synchronized while it catches up after
			// being offline; busy_syncing is what gives it away.
			name: "catching up after downtime is still syncing",
			info: monerodInfo{Status: "OK", Synchronized: true, BusySyncing: true},
			want: LocalSyncing,
		},
		{
			name: "synced but without --zmq-pub names the flag",
			info: synced, zmqErr: errors.New("no ZMTP greeting came back"),
			want: LocalNoZMQ, wantDetail: "--zmq-pub tcp://127.0.0.1:18083",
		},
		{name: "an RPC login is reported, not guessed", rpcErr: errRPCLogin, want: LocalLocked, wantDetail: "login"},
		{
			name:   "something that is not monerod is not used",
			rpcErr: errors.New("not a monerod reply: invalid character"),
			want:   LocalBroken,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := judgeLocal("127.0.0.1:18081", "127.0.0.1:18083", c.info, c.rpcErr, c.zmqErr)
			if got.State != c.want {
				t.Fatalf("state = %v, want %v (%s)", got.State, c.want, got.Detail)
			}
			if !strings.Contains(got.Detail, c.wantDetail) {
				t.Errorf("detail = %q, want it to mention %q", got.Detail, c.wantDetail)
			}
			if c.want == LocalUsable && got.Node != (Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083}) {
				t.Errorf("node = %+v", got.Node)
			}
		})
	}
}

// fakeMonerod serves get_info the way monerod does.
func fakeMonerod(t *testing.T, result string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":"0","result":`+result+`}`)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func fakeZMQ(t *testing.T) string {
	return serveOnce(t, func(c net.Conn) {
		buf := make([]byte, zmtpGreetingLen)
		if _, err := io.ReadFull(c, buf); err == nil {
			c.Write(zmtpGreeting())
		}
	})
}

func closedPort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestProbeLocalAt(t *testing.T) {
	const syncedJSON = `{"status":"OK","synchronized":true,"busy_syncing":false,"height":3400000,"target_height":3400000}`
	t.Run("a synced node with ZMQ", func(t *testing.T) {
		got := probeLocalAt(fakeMonerod(t, syncedJSON), fakeZMQ(t), time.Second)
		if got.State != LocalUsable {
			t.Errorf("state = %v (%s), want usable", got.State, got.Detail)
		}
	})
	t.Run("a synced node without ZMQ", func(t *testing.T) {
		got := probeLocalAt(fakeMonerod(t, syncedJSON), closedPort(t), time.Second)
		if got.State != LocalNoZMQ {
			t.Errorf("state = %v (%s), want no ZMQ", got.State, got.Detail)
		}
	})
	t.Run("no node at all, and quickly", func(t *testing.T) {
		start := time.Now()
		got := probeLocalAt(closedPort(t), closedPort(t), time.Second)
		if got.State != LocalAbsent {
			t.Errorf("state = %v (%s), want absent", got.State, got.Detail)
		}
		if time.Since(start) > 500*time.Millisecond {
			t.Errorf("took %s to find nothing; startup waits on this", time.Since(start))
		}
	})
	t.Run("a node behind an RPC login", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		got := probeLocalAt(strings.TrimPrefix(srv.URL, "http://"), fakeZMQ(t), time.Second)
		if got.State != LocalLocked {
			t.Errorf("state = %v (%s), want locked", got.State, got.Detail)
		}
	})
}
