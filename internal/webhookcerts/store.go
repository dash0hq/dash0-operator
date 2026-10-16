// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// Store holds the serving certificate and the CA bundle in memory. The webhook server reads the serving certificate
// from the store on every TLS handshake, so a new certificate is used immediately, without waiting for the kubelet to
// update a mounted Secret volume.
type Store struct {
	lock        sync.RWMutex
	certificate *tls.Certificate
	leaf        *x509.Certificate
	caBundle    []byte
	listeners   []func([]byte)

	ready atomic.Bool
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{}
}

// GetCertificate returns the current serving certificate. It is meant to be used as tls.Config.GetCertificate.
func (s *Store) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.certificate == nil {
		return nil, fmt.Errorf("the webhook serving certificate has not been loaded yet")
	}
	return s.certificate, nil
}

// CaBundle returns the PEM-encoded CA bundle, or nil if no material has been loaded yet.
func (s *Store) CaBundle() []byte {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.caBundle
}

// OnCaBundleChange registers a listener that is called with the new CA bundle whenever the bundle changes. Listeners
// are called in a separate goroutine.
func (s *Store) OnCaBundleChange(listener func(caBundle []byte)) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.listeners = append(s.listeners, listener)
}

// ReadyCheck reports an error until the material has been loaded and has been injected once. It has the signature of a
// controller-runtime healthz.Checker.
func (s *Store) ReadyCheck(_ *http.Request) error {
	if !s.ready.Load() {
		return fmt.Errorf("the webhook certificates have not been loaded and injected yet")
	}
	return nil
}

// IsReady reports whether the readiness latch has been set.
func (s *Store) IsReady() bool {
	return s.ready.Load()
}

func (s *Store) setReady() {
	s.ready.Store(true)
}

func (s *Store) servingCertificate() *x509.Certificate {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.leaf
}

func (s *Store) load(m *material) error {
	certificate, err := tls.X509KeyPair(m.serving.certPem, m.serving.keyPem)
	if err != nil {
		return fmt.Errorf("cannot load the serving certificate: %w", err)
	}
	bundle := m.bundle()

	s.lock.Lock()
	s.certificate = &certificate
	s.leaf = m.serving.cert
	bundleChanged := !bytes.Equal(s.caBundle, bundle)
	s.caBundle = bundle
	s.lock.Unlock()

	if bundleChanged {
		s.notifyListeners()
	}
	return nil
}

// notifyListeners calls the listeners with the current bundle, also if it has not changed. This is used when an object
// that is kept up to date by a listener has been found to carry an outdated bundle.
func (s *Store) notifyListeners() {
	s.lock.RLock()
	bundle := s.caBundle
	listeners := append([]func([]byte){}, s.listeners...)
	s.lock.RUnlock()
	for _, listener := range listeners {
		go listener(bundle)
	}
}
