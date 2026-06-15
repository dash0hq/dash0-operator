// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

const (
	// serverAddressEnvVarName is the environment variable through which the operator passes the address of the Dash0
	// backend service this client connects to (set from the Helm value operator.agent0Connector.serverAddress).
	serverAddressEnvVarName = "DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS"

	// insecureEnvVarName is the environment variable through which the operator can disable TLS for the connection to
	// the Dash0 backend (set from the Helm value operator.agent0Connector.insecure). When it is "true" the client
	// connects via plaintext; otherwise it connects via TLS, verifying the server certificate against the host's system
	// root CA pool. Disabling TLS is only intended for local development.
	insecureEnvVarName = "DASH0_AGENT0_CONNECTOR_INSECURE"

	// metadataClientID is the gRPC metadata key via which the client announces its unique client ID to the Dash0 backend.
	metadataClientID = "dash0-client-id"

	// metadataAuthorization is the gRPC metadata key via which the client sends its Dash0 authorization token to the
	// backend. The value is the authorization token prefixed with "Bearer ".
	metadataAuthorization = "authorization"

	// authTokenEnvVarName is the environment variable through which the operator passes the Dash0 authorization token
	// (set from the Helm value operator.agent0Connector.token, or resolved from operator.agent0Connector.secretRef via a
	// Kubernetes secret reference). The token is mandatory; if it is not set the process logs an error and exits.
	authTokenEnvVarName = "DASH0_AGENT0_CONNECTOR_AUTH_TOKEN"

	// clusterUidEnvVarName is the environment variable through which the operator passes the pseudo cluster UID
	// (the UID of the default namespace, equal to the k8s.cluster.uid resource attribute). It is used as the
	// client ID when subscribing to command requests.
	clusterUidEnvVarName = "K8S_CLUSTER_UID"

	// initialReconnectDelay is the delay before the first reconnect attempt after a stream drops. Subsequent attempts
	// back off exponentially up to maxReconnectDelay.
	initialReconnectDelay = 1 * time.Second

	// maxReconnectDelay caps the exponential reconnect backoff.
	maxReconnectDelay = 5 * time.Minute

	// healthyStreamThreshold is the minimum time a stream must have stayed up to be considered healthy. After a healthy
	// stream drops, the backoff is reset to initialReconnectDelay so a single blip does not inherit a long delay that
	// accumulated during an earlier outage.
	healthyStreamThreshold = 1 * time.Minute
)

// runSubscriber opens the SubscribeToCommandRequests stream to the backend and keeps it open, reconnecting
// whenever the stream drops, until the provided context is cancelled (e.g. on shutdown).
func runSubscriber(ctx context.Context, logger *slog.Logger) {
	serverAddress := resolveServerAddress(logger)
	transportCredentials := resolveTransportCredentials(logger)
	clientID := resolveClientID(logger)
	authToken := resolveAuthToken(logger)
	kubectlTmpDir := resolveKubectlTmpDir(logger)
	logger.Info("connecting to the Dash0 backend", "address", serverAddress, "clientId", clientID)

	reconnectDelay := initialReconnectDelay
	for ctx.Err() == nil {
		streamStart := time.Now()
		err := runStream(ctx, logger, serverAddress, transportCredentials, clientID, authToken, kubectlTmpDir)
		if ctx.Err() != nil {
			return
		}

		// Reset the backoff after a stream that stayed up long enough to be considered healthy, so a single blip does
		// not inherit a long delay that accumulated during an earlier outage.
		if time.Since(streamStart) >= healthyStreamThreshold {
			reconnectDelay = initialReconnectDelay
		}

		// Always wait before reconnecting, reconnecting immediately would spin in a tight loop. The delay is jittered to
		// avoid synchronized reconnect storms across many agent0-connector pods.
		wait := jitter(reconnectDelay)
		if err != nil {
			logger.Warn("command request stream ended, reconnecting", "error", err, "retryIn", wait)
		} else {
			logger.Info("command request stream closed by the backend, reconnecting", "retryIn", wait)
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}

		reconnectDelay = nextReconnectDelay(reconnectDelay)
	}
}

// nextReconnectDelay doubles the current reconnect delay, capped at maxReconnectDelay.
func nextReconnectDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > maxReconnectDelay {
		return maxReconnectDelay
	}
	return next
}

// jitter reduces d by a random amount of up to 20%, returning a value in [0.8*d, d]. Spreading reconnect attempts this
// way prevents many clients that dropped at the same time from reconnecting in lockstep.
func jitter(d time.Duration) time.Duration {
	delta := d / 5
	if delta <= 0 {
		return d
	}
	return d - time.Duration(rand.Int64N(int64(delta)))
}

// resolveServerAddress returns the address of the Dash0 backend service, read from the
// DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS environment variable. The address is mandatory; if it is not set the process
// logs an error and exits, since the client has nothing to connect to. (When the operator deploys this workload, the
// address is always provided via the Helm value operator.agent0Connector.serverAddress.)
func resolveServerAddress(logger *slog.Logger) string {
	serverAddress := os.Getenv(serverAddressEnvVarName)
	if serverAddress == "" {
		logger.Error(
			"the server address environment variable is not set, cannot connect to the Dash0 backend",
			"envVar", serverAddressEnvVarName,
		)
		os.Exit(1)
	}
	return serverAddress
}

// resolveTransportCredentials returns the gRPC transport credentials used to connect to the Dash0 backend. By default
// it returns TLS credentials that verify the server certificate against the host's system root CA pool, using the host
// part of the server address for hostname verification (SNI). If the DASH0_AGENT0_CONNECTOR_INSECURE environment
// variable is "true", it returns plaintext credentials instead; this is only intended for local development.
func resolveTransportCredentials(logger *slog.Logger) credentials.TransportCredentials {
	if strings.EqualFold(os.Getenv(insecureEnvVarName), "true") {
		logger.Warn(
			"connecting to the Dash0 backend without TLS (insecure transport); this should only be used for local " +
				"development",
		)
		return insecure.NewCredentials()
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
}

// resolveClientID returns the client ID announced to the backend. It is the pseudo cluster UID passed in via the
// K8S_CLUSTER_UID environment variable. The cluster UID is mandatory; if it is not set the process logs an error and
// exits. (When the operator deploys this workload, the cluster UID is always provided.)
func resolveClientID(logger *slog.Logger) string {
	clientID := os.Getenv(clusterUidEnvVarName)
	if clientID == "" {
		logger.Error(
			"the cluster UID environment variable is not set, cannot connect to the Dash0 backend",
			"envVar", clusterUidEnvVarName,
		)
		os.Exit(1)
	}
	return clientID
}

// resolveAuthToken returns the Dash0 authorization token, read from the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN environment
// variable. The token is mandatory; if it is not set the process logs an error and exits, since the Dash0 backend
// rejects unauthenticated connections. (When the operator deploys this workload, the token is always provided, either
// from the Helm value operator.agent0Connector.token or resolved from operator.agent0Connector.secretRef via a
// Kubernetes secret reference.)
func resolveAuthToken(logger *slog.Logger) string {
	authToken := os.Getenv(authTokenEnvVarName)
	if authToken == "" {
		logger.Error(
			"the authorization token environment variable is not set, cannot connect to the Dash0 backend",
			"envVar", authTokenEnvVarName,
		)
		os.Exit(1)
	}
	return authToken
}

// resolveKubectlTmpDir returns the writable directory used for kubectl's caches, read once at startup from the
// DASH0_KUBECTL_TMP environment variable. The resolved value is then reused for every kubectl invocation (see
// kubectlEnv) instead of re-reading the environment on each command. The value is mandatory; if it is not set the
// process logs an error and exits, since the container's root filesystem is read-only and kubectl needs a writable
// HOME for its caches. (When the operator deploys this workload the variable is always provided, and the image
// defaults it to /tmp.)
func resolveKubectlTmpDir(logger *slog.Logger) string {
	tmpDir := os.Getenv(kubectlTmpEnvVarName)
	if tmpDir == "" {
		logger.Error(
			"the kubectl tmp directory environment variable is not set, cannot run kubectl with a writable cache directory",
			"envVar", kubectlTmpEnvVarName,
		)
		os.Exit(1)
	}
	return tmpDir
}

// runStream opens a single SubscribeToCommandRequests stream and listens to incomming CommandRequest, until the stream
// fails or the context is cancelled. For every received CommandRequest it executes the requested (read-only) kubectl
// command and sends back the CommandResponse.
func runStream(
	ctx context.Context,
	logger *slog.Logger,
	serverAddress string,
	transportCredentials credentials.TransportCredentials,
	clientID string,
	authToken string,
	kubectlTmpDir string,
) error {
	conn, err := grpc.NewClient(
		serverAddress,
		grpc.WithTransportCredentials(transportCredentials),
		// The command request stream is mostly idle (commands arrive sporadically). Without keepalive, cloud NAT
		// gateways and load balancers might silently drop the idle TCP connection after their idle timeout; stream.Recv
		// would then block indefinitely and commands would never be delivered. Keepalive pings keep the connection alive
		// and surface a dead connection as a stream error that triggers a reconnect.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	client := pb.NewOutboundConnectorServiceClient(conn)

	streamCtx := metadata.AppendToOutgoingContext(
		ctx,
		metadataClientID, clientID,
		metadataAuthorization, "Bearer "+authToken,
	)
	stream, err := client.SubscribeToCommandRequests(streamCtx)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}

	logger.Info("subscribed to command requests")

	return listenToCommandRequests(ctx, logger, stream, kubectlTmpDir)
}

// commandRequestStream is the subset of the gRPC bidirectional stream that listenToCommandRequests needs: receiving
// CommandRequests from the backend and sending CommandResponses back. The generated client stream satisfies it; tests
// provide a fake implementation.
type commandRequestStream interface {
	Recv() (*pb.CommandRequest, error)
	Send(*pb.CommandResponse) error
}

// listenToCommandRequests reads CommandRequests from the stream until it is closed or fails. For every request it
// executes the requested command and sends back the CommandResponse. It returns nil when the backend closed the stream
// cleanly (io.EOF) and a wrapped error on any receive or send failure.
func listenToCommandRequests(
	ctx context.Context,
	logger *slog.Logger,
	stream commandRequestStream,
	kubectlTmpDir string,
) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("stream receive failed: %w", err)
		}

		logger.Info(
			"received command request",
			"requestId", req.GetRequestId(),
			"command", req.GetCommand(),
			"arguments", req.GetArguments(),
		)

		resp := executeCommandRequest(ctx, logger, kubectlTmpDir, req)

		if err := stream.Send(resp); err != nil {
			return fmt.Errorf("stream send failed: %w", err)
		}

		logger.Info(
			"sent command response",
			"requestId", resp.GetRequestId(),
			"exitCode", resp.GetExitCode(),
		)
	}
}
