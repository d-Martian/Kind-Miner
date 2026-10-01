package advisory

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testKey is a key pair in minisign's format, made fresh for each test.
type testKey struct {
	pub  PublicKey
	priv ed25519.PrivateKey
}

func newKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var id [8]byte
	rand.Read(id[:])
	return testKey{PublicKey{ID: id, Key: pub}, priv}
}

// pubFile is the key as minisign writes a .pub file.
func (k testKey) pubFile() string {
	raw := append(append([]byte("Ed"), k.pub.ID[:]...), k.pub.Key...)
	return "untrusted comment: minisign public key\n" + base64.StdEncoding.EncodeToString(raw) + "\n"
}

// sign makes a .minisig the way `minisign -S -l` does.
func (k testKey) sign(msg []byte, trusted string) string {
	s := ed25519.Sign(k.priv, msg)
	raw := append(append([]byte("Ed"), k.pub.ID[:]...), s...)
	global := ed25519.Sign(k.priv, append(append([]byte(nil), s...), trusted...))
	return "untrusted comment: signature\n" + base64.StdEncoding.EncodeToString(raw) +
		"\ntrusted comment: " + trusted + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func manifest(serial int64, extra string) []byte {
	return []byte(fmt.Sprintf(`{"serial":%d,"issued":"2026-10-01T00:00:00Z","expires":"2026-11-01T00:00:00Z"%s}`, serial, extra))
}

func TestMinisign(t *testing.T) {
	k := newKey(t)
	pk, err := ParsePublicKey(k.pubFile())
	if err != nil || pk.ID != k.pub.ID {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	msg := manifest(1, "")
	sig := k.sign(msg, "kind-miner advisory 1")
	if tc, err := pk.Verify(msg, sig); err != nil || tc != "kind-miner advisory 1" {
		t.Fatalf("a good signature: %q, %v", tc, err)
	}
	cases := []struct {
		name     string
		msg, sig string
		want     string
	}{
		{"a changed manifest", string(manifest(2, "")), sig, ""},
		{"a changed trusted comment", string(msg), strings.Replace(sig, "advisory 1", "advisory 9", 1), "trusted comment"},
		{"another key's signature", string(msg), newKey(t).sign(msg, "x"), "different key"},
		{"minisign's default prehashed form", string(msg), strings.Replace(sig, base64.StdEncoding.EncodeToString([]byte("Ed"))[:2], "RU", 1), "minisign -l"},
		{"not a signature at all", string(msg), "hello", "not a minisign signature"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pk.Verify([]byte(c.msg), c.sig)
			if !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want ErrBadSignature mentioning %q", err, c.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name, json, want string
	}{
		{"no serial", `{"issued":"2026-10-01T00:00:00Z","expires":"2026-11-01T00:00:00Z"}`, "no serial"},
		{"expiry before issue", `{"serial":1,"issued":"2026-10-01T00:00:00Z","expires":"2026-09-01T00:00:00Z"}`, "backwards"},
		{"good for longer than a leaked key should be", `{"serial":1,"issued":"2026-10-01T00:00:00Z","expires":"2027-10-01T00:00:00Z"}`, "more than 60 days"},
		{"a floor that is not a version", string(manifest(1, `,"min_versions":{"p2pool":"latest"}`)), "not a version"},
		{"a fork with no height", string(manifest(1, `,"forks":[{"name":"v5","chain":"sidechain","requires":{"p2pool":"4.19"}}]`)), "incomplete"},
		{"a node with no ZMQ port", string(manifest(1, `,"nodes":[{"host":"a.onion","rpc_port":18089}]`)), "incomplete"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestVerifiedRefusesAnOlderSerial(t *testing.T) {
	k := newKey(t)
	msg := manifest(4, "")
	if _, err := Verified(k.pub, msg, k.sign(msg, "4"), 5); err == nil || !strings.Contains(err.Error(), "older") {
		t.Errorf("err = %v; a replayed manifest was accepted", err)
	}
	if m, err := Verified(k.pub, msg, k.sign(msg, "4"), 4); err != nil || m.Serial != 4 {
		t.Errorf("the same serial again: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	m, err := Parse(manifest(7, `,
		"min_versions":{"p2pool":"4.18","xmrig":"6.26.0"},
		"forks":[{"name":"p2pool v5 sidechain fork","chain":"sidechain","height":15000000,"requires":{"p2pool":"4.19"}}],
		"notice":"The default node moves on 1 November."`))
	if err != nil {
		t.Fatal(err)
	}
	now := t0.Add(24 * time.Hour)
	cases := []struct {
		name    string
		now     time.Time
		run     Running
		h       Heights
		stop    string
		warning string
	}{
		{"engines at the floor run, and hear about the fork", now, Running{"p2pool": "4.18", "xmrig": "6.26.0"},
			Heights{Sidechain: 14900000}, "", "update before sidechain height 15000000"},
		{"an engine below the floor stops mining", now, Running{"p2pool": "4.17", "xmrig": "6.26.0"},
			Heights{}, "Update needed: p2pool 4.17 is below the safe minimum, 4.18", ""},
		{"a passed fork stops an engine too old for it", now, Running{"p2pool": "4.18"},
			Heights{Sidechain: 15000001}, "which has passed", ""},
		{"an engine new enough for the fork mines on", now, Running{"p2pool": "4.19"},
			Heights{Sidechain: 15000001}, "", "default node moves"},
		{"an unknown height is not a passed fork", now, Running{"p2pool": "4.18"},
			Heights{}, "", "needs p2pool 4.19 from sidechain height"},
		{"a user's own build has no known version, and is never stopped", now, Running{"p2pool": ""},
			Heights{Sidechain: 15000001}, "", ""},
		{"an expired advisory changes nothing", t0.Add(40 * 24 * time.Hour), Running{"p2pool": "4.10"},
			Heights{}, "", "expired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Evaluate(m, c.now, c.run, c.h)
			if !strings.Contains(v.Stop, c.stop) || (c.stop == "") != (v.Stop == "") {
				t.Errorf("stop = %q, want %q", v.Stop, c.stop)
			}
			if c.warning != "" && !strings.Contains(strings.Join(v.Warnings, " | "), c.warning) {
				t.Errorf("warnings = %q, want one with %q", v.Warnings, c.warning)
			}
		})
	}
}

func TestBelow(t *testing.T) {
	for _, c := range []struct {
		have, want string
		below      bool
	}{
		{"4.18", "4.19", true}, {"4.9", "4.18", true}, {"v4.18", "4.18", false},
		{"6.26.0", "6.26", false}, {"6.26", "6.26.1", true}, {"6.27.0", "6.26.9", false},
		{"custom-build", "4.18", false}, {"4.18", "", false},
	} {
		if got := below(c.have, c.want); got != c.below {
			t.Errorf("below(%q, %q) = %v", c.have, c.want, got)
		}
	}
}

func TestFetchAndStore(t *testing.T) {
	k := newKey(t)
	good := manifest(3, `,"min_versions":{"p2pool":"4.18"}`)
	mux := http.NewServeMux()
	mux.HandleFunc("/good/manifest.json", func(w http.ResponseWriter, r *http.Request) { w.Write(good) })
	mux.HandleFunc("/good/manifest.json.minisig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(k.sign(good, "3"))) })
	// A mirror serving something it was never given.
	mux.HandleFunc("/forged/manifest.json", func(w http.ResponseWriter, r *http.Request) { w.Write(manifest(99, `,"min_versions":{"p2pool":"99"}`)) })
	mux.HandleFunc("/forged/manifest.json.minisig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(k.sign(good, "3"))) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dial := (&net.Dialer{}).DialContext
	ctx := context.Background()

	m, data, sig, err := fetch(ctx, dial, []string{srv.URL + "/forged/manifest.json", srv.URL + "/missing/manifest.json", srv.URL + "/good/manifest.json"}, k.pub, 0)
	if err != nil || m.Serial != 3 {
		t.Fatalf("a good source after bad ones: %v, %+v", err, m)
	}
	if _, _, _, err := fetch(ctx, dial, []string{srv.URL + "/forged/manifest.json"}, k.pub, 0); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a forged manifest: err = %v", err)
	}
	if _, _, _, err := fetch(ctx, dial, []string{srv.URL + "/good/manifest.json"}, k.pub, 4); err == nil {
		t.Error("an older manifest than already seen was accepted")
	}

	st := Store{Dir: t.TempDir()}
	if _, ok, err := st.Load(k.pub); ok || err != nil {
		t.Errorf("an empty store: ok %v, %v", ok, err)
	}
	if err := st.Save(data, sig); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := st.Load(k.pub); !ok || err != nil || got.Serial != 3 {
		t.Errorf("Load: %+v, %v, %v", got, ok, err)
	}
	os.WriteFile(filepath.Join(st.Dir, "advisory.json"), manifest(3, `,"min_versions":{"p2pool":"99"}`), 0o644)
	if _, ok, err := st.Load(k.pub); ok || !errors.Is(err, ErrBadSignature) {
		t.Errorf("a manifest edited on disk loaded: ok %v, %v", ok, err)
	}
}
