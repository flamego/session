// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

var _ Session = (*memorySession)(nil)

// memorySession is an in-memory session.
type memorySession struct {
	*BaseSession

	lock           sync.RWMutex // The mutex to guard accesses to the lastAccessedAt
	lastAccessedAt time.Time    // The last time of the session being accessed

	index int // The index in the heap
}

// newMemorySession returns a new memory session with given session ID.
func newMemorySession(sid string) *memorySession {
	return &memorySession{
		BaseSession: NewBaseSession(sid, nil),
	}
}

func (s *memorySession) LastAccessedAt() time.Time {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.lastAccessedAt
}

func (s *memorySession) SetLastAccessedAt(t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.lastAccessedAt = t
}

var _ Store = (*memoryStore)(nil)

// memoryStore is an in-memory implementation of the session store.
type memoryStore struct {
	nowFunc  func() time.Time // The function to return the current time
	lifetime time.Duration    // The duration to have no access to a session before being recycled

	lock  sync.RWMutex              // The mutex to guard accesses to the heap and index
	heap  []*memorySession          // The heap to be managed by operations of heap.Interface
	index map[string]*memorySession // The index to be managed by operations of heap.Interface
}

// newMemoryStore returns a new memory session store based on given
// configuration.
func newMemoryStore(cfg MemoryConfig) *memoryStore {
	return &memoryStore{
		nowFunc:  cfg.nowFunc,
		lifetime: cfg.Lifetime,
		index:    make(map[string]*memorySession),
	}
}

// Len implements `heap.Interface.Len`. It is not concurrent-safe and is the
// caller's responsibility to ensure they're being guarded by a mutex during any
// heap operation, i.e. heap.Fix, heap.Remove, heap.Push, heap.Pop.
func (s *memoryStore) Len() int {
	return len(s.heap)
}

// Less implements `heap.Interface.Less`. It is not concurrent-safe and is the
// caller's responsibility to ensure they're being guarded by a mutex during any
// heap operation, i.e. heap.Fix, heap.Remove, heap.Push, heap.Pop.
func (s *memoryStore) Less(i, j int) bool {
	return s.heap[i].LastAccessedAt().Before(s.heap[j].LastAccessedAt())
}

// Swap implements `heap.Interface.Swap`. It is not concurrent-safe and is the
// caller's responsibility to ensure they're being guarded by a mutex during any
// heap operation, i.e. heap.Fix, heap.Remove, heap.Push, heap.Pop.
func (s *memoryStore) Swap(i, j int) {
	s.heap[i], s.heap[j] = s.heap[j], s.heap[i]
	s.heap[i].index = i
	s.heap[j].index = j
}

// Push implements `heap.Interface.Push`. It is not concurrent-safe and is the
// caller's responsibility to ensure they're being guarded by a mutex during any
// heap operation, i.e. heap.Fix, heap.Remove, heap.Push, heap.Pop.
func (s *memoryStore) Push(x interface{}) {
	n := s.Len()
	sess := x.(*memorySession)
	sess.index = n
	s.heap = append(s.heap, sess)
	s.index[sess.sid] = sess
}

// Pop implements `heap.Interface.Pop`. It is not concurrent-safe and is the
// caller's responsibility to ensure they're being guarded by a mutex during any
// heap operation, i.e. heap.Fix, heap.Remove, heap.Push, heap.Pop.
func (s *memoryStore) Pop() interface{} {
	n := s.Len()
	sess := s.heap[n-1]

	s.heap[n-1] = nil // Avoid memory leak
	sess.index = -1   // For safety

	s.heap = s.heap[:n-1]
	delete(s.index, sess.sid)
	return sess
}

func (s *memoryStore) Exist(_ context.Context, sid string) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, ok := s.index[sid]
	return ok
}

func (s *memoryStore) Read(_ context.Context, sid string) (Session, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	sess, ok := s.index[sid]
	if ok {
		// Only return the session if it is not expired, because the GC may have not
		// caught up.
		if s.nowFunc().Before(sess.LastAccessedAt().Add(s.lifetime)) {
			sess.SetLastAccessedAt(s.nowFunc())
			heap.Fix(s, sess.index)
			return sess, nil
		}

		heap.Remove(s, sess.index)
	}

	sess = newMemorySession(sid)
	sess.SetLastAccessedAt(s.nowFunc())
	heap.Push(s, sess)
	return sess, nil
}

func (s *memoryStore) Destroy(_ context.Context, sid string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	sess, ok := s.index[sid]
	if !ok {
		return nil
	}

	heap.Remove(s, sess.index)
	return nil
}

func (s *memoryStore) Save(context.Context, Session) error {
	return nil
}

func (s *memoryStore) GC(ctx context.Context) error {
	// Removing expired sessions from top of the heap until there is no more expired
	// sessions found.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		done := func() bool {
			s.lock.Lock()
			defer s.lock.Unlock()

			if s.Len() == 0 {
				return true
			}

			sess := s.heap[0]

			// If the least accessed session is not expired, there is no need to continue
			if s.nowFunc().Before(sess.LastAccessedAt().Add(s.lifetime)) {
				return true
			}

			heap.Remove(s, sess.index)
			return false
		}()
		if done {
			break
		}
	}
	return nil
}

// MemoryConfig contains options for the memory session store.
type MemoryConfig struct {
	nowFunc func() time.Time // For tests only

	// Lifetime is the duration to have no access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
}

// MemoryIniter returns the Initer for the memory session store.
func MemoryIniter() Initer {
	return func(_ context.Context, args ...interface{}) (Store, error) {
		var cfg *MemoryConfig
		for i := range args {
			switch v := args[i].(type) {
			case MemoryConfig:
				cfg = &v
			}
		}

		if cfg == nil {
			cfg = &MemoryConfig{}
		}

		if cfg.nowFunc == nil {
			cfg.nowFunc = time.Now
		}
		if cfg.Lifetime.Seconds() < 1 {
			cfg.Lifetime = 3600 * time.Second
		}

		return newMemoryStore(*cfg), nil
	}
}
