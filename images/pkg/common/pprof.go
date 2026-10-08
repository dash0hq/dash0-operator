// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common // import "github.com/dash0hq/dash0-operator/images/pkg/common"

import (
	"errors"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
)

const PprofPortEnvVarName = "DASH0_PPROF_PORT"

// PprofLogger is the subset of logging methods StartPprofServerIfConfigured needs. It is satisfied by *slog.Logger as
// is; a logger with a different shape (like the operator manager's logr-based logd.Logger, whose Error method takes
// the error as its first argument) needs a small adapter.
type PprofLogger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// StartPprofServerIfConfigured starts an HTTP server serving the pprof endpoints on the port given via the environment
// variable DASH0_PPROF_PORT, if that variable is set to a non-empty value. If the environment variable is unset or
// empty, no server is started.
//
// The server binds the loopback address 127.0.0.1 explicitly, and is therefore not reachable via the pod's IP address
// (which would make the profiling endpoints available to every pod in the cluster). Every pod network namespace has
// 127.0.0.1 regardless of the cluster's IP family (i.e. ipv4 vs ipv6), and it is the address the container runtime
// connects to when serving kubectl port-forward, so port-forwarding to this server works on any cluster. (Connecting
// via kubectl port-forward is the way the public operator docs recommend for collecting pprof snapshots.)
func StartPprofServerIfConfigured(logger PprofLogger) {
	pprofPort := os.Getenv(PprofPortEnvVarName)
	if pprofPort == "" {
		return
	}
	go func() {
		logger.Warn(
			"starting pprof server (do not use in production unless instructed by Dash0 support to do so)",
			"port",
			pprofPort,
		)
		if err := http.ListenAndServe(net.JoinHostPort("127.0.0.1", pprofPort), nil); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				logger.Info("pprof server has been closed")
			} else {
				logger.Error("error in pprof server", "error", err)
			}
		}
	}()
}
