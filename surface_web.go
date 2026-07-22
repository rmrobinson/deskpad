package deskpad

import "sync"

// WebSurface stores rendered state for HTTP clients and broadcasts updates.
type WebSurface struct {
	lock        sync.RWMutex
	snapshot    Snapshot
	subscribers map[chan Snapshot]struct{}
}

func NewWebSurface() *WebSurface {
	return &WebSurface{
		subscribers: make(map[chan Snapshot]struct{}),
	}
}

func (s *WebSurface) ID() string {
	return "web"
}

func (s *WebSurface) KeyCount() int {
	return 0
}

func (s *WebSurface) Refresh(snapshot Snapshot) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.snapshot = cloneSnapshot(snapshot)
	s.broadcastLocked()
	return nil
}

func (s *WebSurface) UpdateKey(snapshot Snapshot, keyID int) error {
	return s.Refresh(snapshot)
}

func (s *WebSurface) Clear() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.snapshot.Keys = nil
	s.broadcastLocked()
	return nil
}

func (s *WebSurface) Snapshot() Snapshot {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return cloneSnapshot(s.snapshot)
}

// Subscribe returns a channel which receives the current state and future visible state updates.
func (s *WebSurface) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 8)

	s.lock.Lock()
	s.subscribers[ch] = struct{}{}
	ch <- cloneSnapshot(s.snapshot)
	s.lock.Unlock()

	cancel := func() {
		s.lock.Lock()
		defer s.lock.Unlock()

		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
	}

	return ch, cancel
}

func (s *WebSurface) broadcastLocked() {
	snapshot := cloneSnapshot(s.snapshot)
	for ch := range s.subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}
