package monitor

// Session reports whether the desktop session is locked.
//
// It backs the "mine only when the screen is locked" setting, for people who
// want the machine earning while they are away from it and completely untouched
// while they are at it.
type Session struct{ backend lockBackend }

// lockBackend is one way of asking the desktop whether it is locked. Platforms
// with several (Linux has GNOME's and freedesktop's) probe them in turn.
type lockBackend interface {
	locked() (bool, error)
	name() string
}

// NewSession returns a session-lock monitor. Probing happens on first use
// rather than here, so constructing one never blocks on a bus that is not
// there.
func NewSession() *Session { return &Session{} }

// Locked reports whether the session is locked. known is false where the lock
// state cannot be determined — no supported backend, or no session at all — in
// which case callers must not treat "not locked" as a fact.
func (s *Session) Locked() (locked, known bool) {
	if s == nil {
		return false, false
	}
	if s.backend == nil {
		s.backend = probeLockBackend()
		if s.backend == nil {
			return false, false
		}
	}
	v, err := s.backend.locked()
	if err != nil {
		// A backend that worked and then failed (bus restarted, session ended)
		// is dropped so the next call re-probes instead of failing forever.
		s.backend = nil
		return false, false
	}
	return v, true
}
