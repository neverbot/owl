package config

import (
	"sync"
	"sync/atomic"
)

// Manager holds the live Config snapshot and notifies subscribers when it
// changes. The snapshot is exchanged atomically; readers never block.
type Manager struct {
	current atomic.Pointer[Config]

	mu   sync.Mutex
	subs []chan struct{}
}

// NewManager seeds the manager with an initial config snapshot.
func NewManager(initial Config) *Manager {
	m := &Manager{}
	m.current.Store(&initial)
	return m
}

// Snapshot returns the current Config. The returned value is a copy by
// virtue of the atomic load returning a pointer to an immutable value.
// Callers must not mutate the returned Config.
func (m *Manager) Snapshot() *Config {
	return m.current.Load()
}

// Swap atomically replaces the live snapshot and notifies all subscribers.
// Subscribers that are not actively reading their channel are skipped
// (non-blocking send): they will see the new snapshot next time they call
// Snapshot(), and will be notified on the next Swap they happen to drain.
func (m *Manager) Swap(c Config) {
	m.current.Store(&c)

	m.mu.Lock()
	subs := append([]chan struct{}(nil), m.subs...)
	m.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Subscribe returns a channel that receives an empty struct after each
// successful Swap. The channel has a small buffer so a single missed read
// does not silently drop a notification.
func (m *Manager) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	return ch
}
