// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	slogmulti "github.com/samber/slog-multi"
	"go.opentelemetry.io/contrib/bridges/otelslog"

	"github.com/dash0hq/dash0-operator/images/agent0-connector/grpc"
	"github.com/dash0hq/dash0-operator/images/agent0-connector/selfmonitoring"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

const (
	serviceName   = "agent0-connector"
	containerName = "agent0-connector"
)

// The agent0-connector executable executes read-only kubectl commands from within the cluster on behalf of an upstream
// system (e.g. Agent0). The executable is deployed by the operator with a service account that has read-only
// (get & list) permissions. It connects to the Dash0 backend via a bidirectional gRPC stream, receives command
// requests, executes them, and sends back the results as command responses.
func main() {
	ctx := context.Background()

	stdOutSlogHandler := slog.NewJSONHandler(os.Stdout, nil)
	var logger *slog.Logger
	if common.OTelSDKIsConfigured() {
		logger = slog.New(
			slogmulti.Fanout(
				stdOutSlogHandler,
				otelslog.NewHandler(serviceName),
			),
		)
	} else {
		logger = slog.New(stdOutSlogHandler)
	}
	logger.Info("dash0 agent0-connector starting up")

	meter := common.InitOTelSdkFromEnvVars(ctx, selfmonitoring.MeterName, serviceName, containerName)
	selfmonitoring.InitializeMetrics(meter, logger)
	// Deliberately shutting down with the base context: the context below is cancelled when the termination signal
	// arrives, and a cancelled context would keep the OTel SDK from flushing the telemetry it still holds.
	defer common.ShutDownOTelSdk(ctx)

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Subscribe to command requests and execute them until a termination signal is received.
	grpc.RunSubscriber(signalCtx, logger)

	logger.Info("dash0 agent0-connector received a termination signal, shutting down")
}
