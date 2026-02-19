// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import "sync"

// SynchronizedSlice provides a thread-safe storage backed by a slice.
// T must be a pure value type (no pointers, slices, maps, interfaces, or channels
// at any nesting level) to guarantee that Get returns an independent copy.
type SynchronizedSlice[T any] struct {
	mu     sync.RWMutex
	values []T
}

// NewSynchronizedSlice creates a new empty SynchronizedSlice.
func NewSynchronizedSlice[T any]() *SynchronizedSlice[T] {
	return &SynchronizedSlice[T]{}
}

// Get returns a copy of the current slice contents.
// Returns nil if the receiver is nil or no values are stored.
func (s *SynchronizedSlice[T]) Get() []T {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.values == nil {
		return nil
	}
	cp := make([]T, len(s.values))
	copy(cp, s.values)
	return cp
}

// Set replaces the stored slice with a copy of the provided values.
// Passing nil is equivalent to Clear.
func (s *SynchronizedSlice[T]) Set(values []T) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if values == nil {
		s.values = nil
		return
	}
	s.values = make([]T, len(values))
	copy(s.values, values)
}

// Clear removes all elements from the slice.
func (s *SynchronizedSlice[T]) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = nil
}

// SynchronizedMapSlice provides thread-safe storage of slices keyed by string.
// T must be a pure value type (no pointers, slices, maps, interfaces, or channels
// at any nesting level) to guarantee that Get returns an independent copy.
type SynchronizedMapSlice[T any] struct {
	mu     sync.RWMutex
	values map[string][]T
}

// NewSynchronizedMapSlice creates a new empty SynchronizedMapSlice.
func NewSynchronizedMapSlice[T any]() *SynchronizedMapSlice[T] {
	return &SynchronizedMapSlice[T]{}
}

// Get returns a copy of the slice stored at the given key.
func (s *SynchronizedMapSlice[T]) Get(key string) ([]T, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.values == nil {
		return nil, false
	}
	v, exists := s.values[key]
	if !exists {
		return nil, false
	}
	cp := make([]T, len(v))
	copy(cp, v)
	return cp, true
}

// Set replaces the slice at the given key with a copy of the provided values.
func (s *SynchronizedMapSlice[T]) Set(key string, values []T) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string][]T)
	}
	if values == nil {
		delete(s.values, key)
		return
	}
	cp := make([]T, len(values))
	copy(cp, values)
	s.values[key] = cp
}

// Delete removes the entry for the given key.
func (s *SynchronizedMapSlice[T]) Delete(key string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		return
	}
	delete(s.values, key)
}

// NamespaceMutex provides per-key locking so that operations on different
// keys can proceed in parallel while operations on the same key are serialized.
type NamespaceMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewNamespaceMutex creates a new empty NamespaceMutex.
func NewNamespaceMutex() *NamespaceMutex {
	return &NamespaceMutex{}
}

// Lock acquires the mutex for the given key, creating it if necessary.
func (n *NamespaceMutex) Lock(key string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.locks == nil {
		n.locks = make(map[string]*sync.Mutex)
	}
	m, exists := n.locks[key]
	if !exists {
		m = &sync.Mutex{}
		n.locks[key] = m
	}
	n.mu.Unlock()
	m.Lock()
}

// Unlock releases the mutex for the given key.
func (n *NamespaceMutex) Unlock(key string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	m, exists := n.locks[key]
	n.mu.Unlock()
	if !exists {
		return
	}
	m.Unlock()
}

// Delete removes the mutex for the given key.
// The caller must ensure no goroutine holds or is waiting on the lock for this key.
func (n *NamespaceMutex) Delete(key string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.locks == nil {
		return
	}
	delete(n.locks, key)
}
