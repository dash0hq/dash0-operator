// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// The agent0-connector executable executes read-only kubectl commands from within the cluster on behalf of an upstream
// system (e.g. Agent0). The executable is deployed by the operator with a service account that has read-only
// (get & list) permissions. It connects to the Dash0 backend via a bidirectional gRPC stream, receives command
// requests, executes them, and sends back the results as command respsonses.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("dash0 agent0-connector starting up")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Subscribe to command requests and execute them until a termination signal is received.
	runSubscriber(ctx, logger)

	logger.Info("dash0 agent0-connector received a termination signal, shutting down")
}
