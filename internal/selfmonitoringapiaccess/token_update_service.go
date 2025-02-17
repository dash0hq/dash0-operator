// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/go-logr/logr"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type AuthTokenClient interface {
	SetAuthToken(context.Context, string, *logr.Logger)
	RemoveAuthToken(context.Context, *logr.Logger)
}

type TokenUpdateService struct {
	port             string
	server           *http.Server
	authTokenClients []AuthTokenClient
}

const (
	certDir = "/tmp/k8s-webhook-server/serving-certs"

	logPrefix = "update token service"
)

var (
	tlsCert = fmt.Sprintf("%s/tls.crt", certDir)
	tlsKey  = fmt.Sprintf("%s/tls.key", certDir)
)

func NewTokenUpdateService(port string, authTokenClients []AuthTokenClient) *TokenUpdateService {
	return &TokenUpdateService{
		port:             port,
		authTokenClients: authTokenClients,
	}
}

func (s *TokenUpdateService) updateAuthToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := crlog.FromContext(ctx)
	logger.Info(fmt.Sprintf("%s: processing /update-auth-token request", logPrefix))
	defer func() {
		_ = r.Body.Close()
	}()

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, fmt.Sprintf("%s: processing /update-auth-token request, cannot read request payload -> HTTP 400", logPrefix))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	authToken := string(payload)
	if authToken == "" {
		logger.Info(fmt.Sprintf("%s: processing /update-auth-token request, received auth token was empty -> HTTP 400", logPrefix))
		w.WriteHeader(http.StatusBadRequest)
	} else {
		logger.Info(fmt.Sprintf("%s: processing /update-auth-token request, received auth token", logPrefix))
		for _, client := range s.authTokenClients {
			client.SetAuthToken(ctx, authToken, &logger)
		}
		w.WriteHeader(http.StatusOK)
	}
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := crlog.FromContext(ctx)
	logger.Info(fmt.Sprintf("%s: processing %s -> 404", logPrefix, r.URL.Path))
	w.WriteHeader(http.StatusNotFound)
}

func (s *TokenUpdateService) Start(logger *logr.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/update-auth-token", func(w http.ResponseWriter, r *http.Request) {
		s.updateAuthToken(w, r)
	})
	mux.HandleFunc("/", catchAll)

	ctx, cancelCtx := context.WithCancel(context.Background())
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%s", s.port),
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		err := s.server.ListenAndServeTLS(tlsCert, tlsKey)
		if errors.Is(err, http.ErrServerClosed) {
			logger.Info(fmt.Sprintf("%s: server has been closed", logPrefix))
		} else if err != nil {
			logger.Error(err, fmt.Sprintf("%s: error while listening", logPrefix))
		}
		cancelCtx()
	}()
}

func (s *TokenUpdateService) Stop(logger *logr.Logger) {
	if s.server != nil {
		if err := s.server.Close(); err != nil {
			logger.Error(err, fmt.Sprintf("%s: error closing the server in Stop()", logPrefix))
		}
	}
}
