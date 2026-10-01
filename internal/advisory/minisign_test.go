package advisory

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// testdata holds a manifest signed by the real minisign 0.12 with a throwaway
// key (minisign -G -W): once with -l, as scripts/sign-advisory.sh signs, and
// once with minisign's default, which hashes the file with BLAKE2b first.
// What the Go verifier accepts must be exactly what the tool makes.
func TestRealMinisignSignatures(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	pk, err := ParsePublicKey(read("test.pub"))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(read("manifest.json"))

	trusted, err := pk.Verify(msg, read("manifest.json.minisig"))
	if err != nil || trusted != "kind-miner advisory serial 1" {
		t.Fatalf("minisign -l signature: %q, %v", trusted, err)
	}
	if _, err := Verified(pk, msg, read("manifest.json.minisig"), 0); err != nil {
		t.Errorf("Verified: %v", err)
	}
	_, err = pk.Verify(msg, read("manifest.json.prehashed.minisig"))
	if !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "minisign -l") {
		t.Errorf("a default (prehashed) signature: err = %v, want a pointer to -l", err)
	}
	if _, err := pk.Verify(append(msg, ' '), read("manifest.json.minisig")); !errors.Is(err, ErrBadSignature) {
		t.Errorf("one byte added still verified: %v", err)
	}
}
