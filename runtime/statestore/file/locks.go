package file

import "sync"

// convLock returns a per-conv mutex. The lock map itself is guarded by s.mu;
// the map mutex is held only long enough to fetch/create the inner mutex.
// Callers actually hold the inner mutex during file IO.
func (s *Store) convLock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.locks[id]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.locks[id] = m
	return m
}
