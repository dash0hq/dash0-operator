// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"sync"
)

// Mru is a simple, go-routine safe, ordered, most-recently-used cache.
type Mru[T any] struct {
	elements []T
	limit    int
	mu       sync.RWMutex
}

const (
	defaultSizeLimit = 1500
)

func NewMruWithDefaultSizeLimit[T any]() *Mru[T] {
	return NewMru[T](defaultSizeLimit)
}

func NewMru[T any](limit int) *Mru[T] {
	return &Mru[T]{
		limit:    limit,
		elements: make([]T, 0),
	}
}

func (mru *Mru[T]) Put(element T) {
	mru.mu.Lock()
	defer mru.mu.Unlock()
	if len(mru.elements) == mru.limit {
		mru.elements = mru.elements[1:]
	}
	mru.elements = append(mru.elements, element)
}

func (mru *Mru[T]) Take() *T {
	mru.mu.Lock()
	defer mru.mu.Unlock()
	if len(mru.elements) == 0 {
		return nil
	}
	el := mru.elements[0]
	mru.elements = mru.elements[1:]
	return &el
}

func (mru *Mru[T]) ForAllAndClear(f func(T)) {
	mru.mu.Lock()
	defer mru.mu.Unlock()
	for _, element := range mru.elements {
		f(element)
	}
	mru.elements = make([]T, 0)
}

func (mru *Mru[T]) Len() int {
	mru.mu.RLock()
	defer mru.mu.RUnlock()
	return len(mru.elements)
}

func (mru *Mru[T]) IsEmpty() bool {
	mru.mu.RLock()
	defer mru.mu.RUnlock()
	return len(mru.elements) == 0
}
