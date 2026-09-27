package monitor

import (
	"errors"
	"testing"
)

type fakeGameMode struct {
	n     int
	err   error
	calls int
}

func (f *fakeGameMode) clientCount() (int, error) { f.calls++; return f.n, f.err }

func TestGameModeActive(t *testing.T) {
	cases := []struct {
		name              string
		backend           gameModeBackend
		wantActive, known bool
	}{
		{"a game registered", &fakeGameMode{n: 1}, true, true},
		{"daemon running, no game", &fakeGameMode{n: 0}, false, true},
		{"no daemon is unknown, never a game", &fakeGameMode{err: errors.New("name has no owner")}, false, false},
		{"no backend on this platform", nil, false, false},
	}
	for _, c := range cases {
		g := &GameMode{backend: c.backend}
		if active, known := g.Active(); active != c.wantActive || known != c.known {
			t.Errorf("%s: Active = %v, %v; want %v, %v", c.name, active, known, c.wantActive, c.known)
		}
	}
}

func TestGameModeAnswerIsCached(t *testing.T) {
	f := &fakeGameMode{n: 1}
	g := &GameMode{backend: f}
	for i := 0; i < 5; i++ {
		g.Active()
	}
	if f.calls != 1 {
		t.Errorf("asked the daemon %d times within the cache window, want once", f.calls)
	}
}

func TestPSIAvg10ReadsTheFullLine(t *testing.T) {
	const text = "some avg10=12.50 avg60=3.00 avg300=1.00 total=9\nfull avg10=7.25 avg60=2.00 avg300=0.50 total=5\n"
	if v, ok := psiAvg10(text, "full"); !ok || v != 7.25 {
		t.Errorf("full avg10 = %v, %v", v, ok)
	}
	if v, ok := psiAvg10(text, "some"); !ok || v != 12.5 {
		t.Errorf("some avg10 = %v, %v", v, ok)
	}
}
