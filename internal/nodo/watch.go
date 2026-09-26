package nodo

import (
	"log"
	"time"
)

// settle is how long config.json must stay quiet before it is re-read. Nodo's
// SSH UI rewrites the file in place — truncate, then write — so the first
// event can land on an empty or half-written file, and one save produces a
// burst of events. Waiting out the burst means reading the file once, whole.
var settle = time.Second

// Watch calls onChange whenever the owner changes a setting in Node — through
// any of Nodo's UIs — until stop is closed. last is the configuration the
// caller is currently running with, so a save that changes nothing else p2pool
// cares about (the ticker currency, say) does not restart anything.
//
// It returns an error only when watching cannot start at all; where the
// platform has no file watcher there is no Nodo, and it says so.
func Watch(stop <-chan struct{}, last Node, onChange func(Node)) error {
	events, closeWatch, err := watchFile(ConfigPath)
	if err != nil {
		return err
	}
	go func() {
		<-stop
		closeWatch()
	}()
	follow(ConfigPath, events, last, onChange)
	return nil
}

// follow turns a stream of raw file events into settled re-reads, and reports
// each one that changes the node settings. It returns when events closes.
//
// A read that fails to parse is skipped, not reported: it is almost always the
// middle of a write, and the event that finishes the write follows it. Acting
// on it would restart p2pool against a guessed configuration.
func follow(path string, events <-chan struct{}, last Node, onChange func(Node)) {
	var quiet <-chan time.Time
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
			quiet = time.After(settle)
		case <-quiet:
			quiet = nil
			n, ok, err := detectAt(path)
			if err != nil {
				log.Printf("nodo: ignoring an unreadable config.json for now: %v", err)
				continue
			}
			if !ok || n == last {
				continue
			}
			last = n
			onChange(n)
		}
	}
}
