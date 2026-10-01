package advisory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/net/proxy"
)

// Sources are where the manifest is fetched from, in order, always through
// Tor. An onion of the project's own goes first once there is one; GitHub
// Pages is the mirror. Each is the manifest's URL; its signature is beside it
// with ".minisig" appended.
var Sources = []string{
	"https://d-martian.github.io/Kind-Miner/advisory/manifest.json",
}

// maxSize bounds a download. A manifest is a few hundred bytes.
const maxSize = 64 << 10

// Fetch gets the manifest from the first source that serves one that
// verifies, through the SOCKS proxy at socks. A source that fails, or serves
// something that does not verify, is passed over for the next. It returns
// the manifest and the two files, for Store.
func Fetch(ctx context.Context, socks string, pk PublicKey, floor int64) (Manifest, []byte, string, error) {
	// Its own circuit, keyed on what it is, like every other kind of
	// connection kind-miner makes through Tor: fetching the advisory says
	// nothing about which node or hub this machine uses.
	d, err := proxy.SOCKS5("tcp", socks, &proxy.Auth{User: "kind-miner/advisory", Password: "kind-miner"}, proxy.Direct)
	if err != nil {
		return Manifest{}, nil, "", err
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return Manifest{}, nil, "", errors.New("SOCKS5 dialer cannot take a deadline")
	}
	return fetch(ctx, cd.DialContext, Sources, pk, floor)
}

func fetch(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), sources []string, pk PublicKey, floor int64) (Manifest, []byte, string, error) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: dial}}
	defer client.CloseIdleConnections()

	var errs []error
	for _, src := range sources {
		data, err := get(ctx, client, src)
		var sig []byte
		if err == nil {
			sig, err = get(ctx, client, src+".minisig")
		}
		var m Manifest
		if err == nil {
			m, err = Verified(pk, data, string(sig), floor)
		}
		if err == nil {
			return m, data, string(sig), nil
		}
		host := src
		if u, perr := url.Parse(src); perr == nil {
			host = u.Host
		}
		errs = append(errs, fmt.Errorf("%s: %w", host, err))
	}
	return Manifest{}, nil, "", errors.Join(errs...)
}

func get(ctx context.Context, client *http.Client, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, errors.New("larger than any advisory")
	}
	return data, nil
}

// Store keeps the last manifest that verified, so a restart — or a week of
// failed fetches — still knows what it said, and so its serial is the floor
// for the next one.
type Store struct{ Dir string }

func (s Store) paths() (string, string) {
	return filepath.Join(s.Dir, "advisory.json"), filepath.Join(s.Dir, "advisory.json.minisig")
}

// Save keeps a manifest and its signature, signature last: Load finds the
// pair only once both are whole.
func (s Store) Save(data []byte, sig string) error {
	mp, sp := s.paths()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{mp, data}, {sp, []byte(sig)}} {
		if err := os.WriteFile(f.path+".tmp", f.data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(f.path+".tmp", f.path); err != nil {
			return err
		}
	}
	return nil
}

// Load returns the kept manifest, verified again — the file is on disk, and
// disk is not where trust comes from. ok is false when there is none.
func (s Store) Load(pk PublicKey) (m Manifest, ok bool, err error) {
	mp, sp := s.paths()
	data, err := os.ReadFile(mp)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, err
	}
	sig, err := os.ReadFile(sp)
	if err != nil {
		return Manifest{}, false, err
	}
	m, err = Verified(pk, data, string(sig), 0)
	if err != nil {
		return Manifest{}, false, err
	}
	return m, true, nil
}
