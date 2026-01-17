// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import "sync"

// NamespacedStore provides thread-safe storage for values keyed by namespace.
type NamespacedStore[T any] struct {
	mu     sync.RWMutex
	values map[string]T
}

// NewNamespacedStore creates a new initialized NamespacedStore.
func NewNamespacedStore[T any]() *NamespacedStore[T] {
	return &NamespacedStore[T]{
		values: make(map[string]T),
	}
}

// Get retrieves the value for a given namespace.
// Returns the value and a boolean indicating whether the value exists.
func (s *NamespacedStore[T]) Get(namespace string) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, exists := s.values[namespace]
	return value, exists
}

// Set sets the value for a given namespace.
func (s *NamespacedStore[T]) Set(namespace string, value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string]T)
	}
	s.values[namespace] = value
}

// Delete removes the value for a given namespace.
func (s *NamespacedStore[T]) Delete(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, namespace)
}
