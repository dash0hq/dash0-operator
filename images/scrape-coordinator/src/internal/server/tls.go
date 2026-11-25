// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/certwatcher"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
)

// newTLSConfig creates the configuration of the TLS listener. It requires a client certificate signed by the CA. The
// returned cert watcher reloads the server certificate when its files change, and the CA along with it, because all
// files come from the same secret volume, which Kubernetes updates atomically. The cert watcher needs to be started.
func newTLSConfig(logger *slog.Logger, files *config.TLS) (*tls.Config, *certwatcher.CertWatcher, error) {
	certWatcher, err := certwatcher.New(files.CertFile, files.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load the server certificate: %w", err)
	}
	var clientCAs atomic.Pointer[x509.CertPool]
	pool, err := loadCertPool(files.CAFile)
	if err != nil {
		return nil, nil, err
	}
	clientCAs.Store(pool)
	certWatcher.RegisterCallback(func(tls.Certificate) {
		reloaded, err := loadCertPool(files.CAFile)
		if err != nil {
			logger.Error("cannot reload the CA certificate, the previous one stays in use", "error", err)
			return
		}
		clientCAs.Store(reloaded)
		logger.Info("the CA certificate has been reloaded")
	})

	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: certWatcher.GetCertificate,
		// The CA pool can change at runtime, so each handshake gets a config with the current one.
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{
				MinVersion:     tls.VersionTLS12,
				GetCertificate: certWatcher.GetCertificate,
				ClientAuth:     tls.RequireAndVerifyClientCert,
				ClientCAs:      clientCAs.Load(),
			}, nil
		},
	}, certWatcher, nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("the CA certificate file %s contains no valid certificate", path)
	}
	return pool, nil
}
