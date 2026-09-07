// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dash0hq/dash0-operator/images/agent0-connector/kubectl"
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
	// (the UID of the kube-system namespace, equal to the k8s.cluster.uid resource attribute). It is used as the
	// client ID when subscribing to command requests.
	clusterUidEnvVarName = "K8S_CLUSTER_UID"

	// maxConcurrentCommandsEnvVarName is the environment variable through which the operator passes how many command
	// requests may be executed at the same time (set from the Helm value
	// operator.agent0Connector.maxConcurrentCommands).
	maxConcurrentCommandsEnvVarName = "DASH0_AGENT0_CONNECTOR_MAX_CONCURRENT_COMMANDS"

	// defaultMaxConcurrentCommands is the number of command requests executed at the same time when the environment
	// variable is absent or does not hold a positive integer. It mirrors defaultMaxConcurrentCommands in
	// internal/agent0connector/a0cresources/desired_state.go, which is where the value comes from when the operator
	// deploys this workload; the connector is a separate Go module, so the two cannot share a constant.
	//
	// The value is bounded by memory, not by CPU: a request that returns the full maxStdoutBytes of output costs about
	// 90 MiB, roughly 50 MiB for the kubectl child process (which GOMEMLIMIT does not govern) and roughly 40 MiB in this
	// process, most of it for parsing the output in order to redact credentials from it. Two of those fit into the
	// default memory limit of the pod.
	defaultMaxConcurrentCommands = 2

	// commandQueueCapacity is how many received command requests wait for a free worker. (A queued request holds only
	// the command and its arguments, so items in the queue are cheap with regard to memory consumption.)
	commandQueueCapacity = 16

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

// toleratedSchemePrefixes are the protocol prefixes that resolveServerAddress accepts and removes from the configured
// server address. Any other scheme is left alone, in particular the schemes of gRPC's own target syntax (dns://,
// passthrough:, unix:), which grpc.NewClient understands.
var toleratedSchemePrefixes = []string{"https://", "http://"}

// RunSubscriber opens the SubscribeToCommandRequests stream to the backend and keeps it open, reconnecting
// whenever the stream drops, until the provided context is cancelled (e.g. on shutdown).
func RunSubscriber(ctx context.Context, logger *slog.Logger) {
	serverAddress := resolveServerAddress(logger)
	transportCredentials := resolveTransportCredentials(logger)
	clientID := resolveClientID(logger)
	authToken := resolveAuthToken(logger)
	kubectlTmpDir := resolveKubectlTmpDir(logger)
	maxConcurrentCommands := resolveMaxConcurrentCommands(logger)
	logger.Info(
		"connecting to the Dash0 backend",
		"address", serverAddress,
		"clientId", clientID,
		"maxConcurrentCommands", maxConcurrentCommands,
	)

	reconnectDelay := initialReconnectDelay
	for ctx.Err() == nil {
		streamStart := time.Now()
		err := runStream(
			ctx,
			logger,
			serverAddress,
			transportCredentials,
			clientID,
			authToken,
			kubectlTmpDir,
			maxConcurrentCommands,
		)
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
// address is always provided via the Helm value operator.agent0Connector.serverAddress.) An address that is configured
// as a URL is reduced to its host and port, see normalizeServerAddress.
func resolveServerAddress(logger *slog.Logger) string {
	serverAddress := os.Getenv(serverAddressEnvVarName)
	if serverAddress == "" {
		logger.Error(
			"the server address environment variable is not set, cannot connect to the Dash0 backend",
			"envVar", serverAddressEnvVarName,
		)
		os.Exit(1)
	}
	serverAddress = normalizeServerAddress(logger, serverAddress)
	if serverAddress == "" {
		logger.Error(
			"the server address environment variable has no host, cannot connect to the Dash0 backend",
			"envVar", serverAddressEnvVarName,
			"value", os.Getenv(serverAddressEnvVarName),
		)
		os.Exit(1)
	}
	return serverAddress
}

// normalizeServerAddress turns a server address that is configured as a URL into a gRPC target, by removing a leading
// http:// or https:// prefix together with everything that follows the host and port. Note that the scheme has no say
// in whether the connection uses TLS, only the DASH0_AGENT0_CONNECTOR_INSECURE environment variable does. An address
// without one of the two prefixes is returned unchanged.
func normalizeServerAddress(logger *slog.Logger, serverAddress string) string {
	prefixLength := 0
	for _, prefix := range toleratedSchemePrefixes {
		if len(serverAddress) >= len(prefix) && strings.EqualFold(serverAddress[:len(prefix)], prefix) {
			prefixLength = len(prefix)
			break
		}
	}
	if prefixLength == 0 {
		return serverAddress
	}

	normalized := serverAddress[prefixLength:]
	if index := strings.IndexAny(normalized, "/?#"); index >= 0 {
		if normalized[index:] != "/" {
			logger.Warn(
				"the configured server address has a path, a query or a fragment, ignoring everything after the host "+
					"and the port",
				"envVar", serverAddressEnvVarName,
				"value", serverAddress,
			)
		}
		normalized = normalized[:index]
	}
	logger.Info(
		"the configured server address has a protocol prefix, using its host and port as the gRPC target",
		"envVar", serverAddressEnvVarName,
		"value", serverAddress,
		"target", normalized,
	)
	return normalized
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
	tmpDir := os.Getenv(kubectl.KubectlTmpEnvVarName)
	if tmpDir == "" {
		logger.Error(
			"the kubectl tmp directory environment variable is not set, cannot run kubectl with a writable cache directory",
			"envVar", kubectl.KubectlTmpEnvVarName,
		)
		os.Exit(1)
	}
	return tmpDir
}

// resolveMaxConcurrentCommands returns how many command requests are executed at the same time, read from the
// DASH0_AGENT0_CONNECTOR_MAX_CONCURRENT_COMMANDS environment variable. An absent variable, or a value that is not a
// positive integer, falls back to defaultMaxConcurrentCommands.
func resolveMaxConcurrentCommands(logger *slog.Logger) int {
	value := os.Getenv(maxConcurrentCommandsEnvVarName)
	if value == "" {
		return defaultMaxConcurrentCommands
	}
	maxConcurrentCommands, err := strconv.Atoi(value)
	if err != nil || maxConcurrentCommands < 1 {
		logger.Warn(
			"the maximum number of concurrent commands is not a positive integer, using the default",
			"envVar", maxConcurrentCommandsEnvVarName,
			"value", value,
			"default", defaultMaxConcurrentCommands,
		)
		return defaultMaxConcurrentCommands
	}
	return maxConcurrentCommands
}

// runStream opens a single SubscribeToCommandRequests stream and listens to incoming CommandRequest, until the stream
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
	maxConcurrentCommands int,
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

	return listenToCommandRequests(
		ctx,
		logger,
		stream,
		kubectlTmpDir,
		maxConcurrentCommands,
		kubectl.ExecuteCommandRequest,
	)
}

// commandRequestStream is the subset of the gRPC bidirectional stream that listenToCommandRequests needs: receiving
// CommandRequests from the backend and sending CommandResponses back. The generated client stream satisfies it; tests
// provide a fake implementation.
type commandRequestStream interface {
	Recv() (*pb.CommandRequest, error)
	Send(*pb.CommandResponse) error
}

// commandExecutor executes a single CommandRequest and returns the CommandResponse to send back.
// kubectl.ExecuteCommandRequest is the implementation used in production; tests provide their own.
type commandExecutor func(
	ctx context.Context,
	logger *slog.Logger,
	kubectlTmpDir string,
	req *pb.CommandRequest,
) *pb.CommandResponse

// listenToCommandRequests reads CommandRequests from the stream until it is closed or fails. Every request is handed to
// a pool of maxConcurrentCommands workers which execute it and hand the CommandResponse to a single sender, so that one
// slow command does not hold up the delivery of the requests behind it. Responses are not necessarily sent in the order
// the requests arrived (the backend correlates them by request ID anyway).
//
// It returns nil when the backend closed the stream cleanly (io.EOF) and a wrapped error on any receive or send
// failure.
func listenToCommandRequests(
	ctx context.Context,
	logger *slog.Logger,
	stream commandRequestStream,
	kubectlTmpDir string,
	maxConcurrentCommands int,
	execute commandExecutor,
) error {
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	requests := make(chan *pb.CommandRequest, commandQueueCapacity)
	// Unbuffered: at most one finished response per worker waits for the sender, which bounds how much captured output
	// is held in memory. A worker that parks here stops taking requests, the queue fills up, and the receive loop stops
	// accepting - that is the intended backpressure.
	responses := make(chan *pb.CommandResponse)

	var workers sync.WaitGroup
	for range maxConcurrentCommands {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for req := range requests {
				responses <- execute(workerCtx, logger, kubectlTmpDir, req)
			}
		}()
	}

	var sendErr error
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		for resp := range responses {
			if sendErr != nil {
				// The stream is broken. Keep draining so that no worker blocks on handing over its response, but do not
				// attempt to send any more.
				logger.Warn(
					"discarding a command response, the stream is broken",
					"requestId", resp.GetRequestId(),
					"exitCode", resp.GetExitCode(),
				)
				continue
			}
			if err := stream.Send(resp); err != nil {
				sendErr = fmt.Errorf("stream send failed: %w", err)
				// The responses of the commands that are still running cannot be delivered any more, so abort them
				// instead of letting them run into their timeout. This also releases the receive loop.
				cancelWorkers()
				logger.Warn(
					"failed to send a command response",
					"requestId", resp.GetRequestId(),
					"error", err,
				)
				continue
			}
			logger.Info(
				"sent command response",
				"requestId", resp.GetRequestId(),
				"exitCode", resp.GetExitCode(),
			)
		}
	}()

	recvErr := receiveCommandRequests(workerCtx, logger, stream, requests)

	if recvErr != nil {
		// The stream is gone, so the responses of the commands that are still running cannot be delivered. Aborting them
		// keeps the reconnect from waiting for up to commandTimeout. A stream the backend closed cleanly (recvErr == nil)
		// lets them finish instead.
		cancelWorkers()
	}
	close(requests)
	workers.Wait()
	close(responses)
	<-senderDone

	if sendErr != nil {
		// The send failure is the more specific diagnosis: it broke the stream that the receive loop then stopped on.
		return sendErr
	}
	return recvErr
}

// receiveCommandRequests reads CommandRequests from the stream and hands them to the worker queue, until the stream is
// closed or fails, or until the context is cancelled. It returns nil when the backend closed the stream cleanly
// (io.EOF) and a wrapped error on a receive failure.
func receiveCommandRequests(
	ctx context.Context,
	logger *slog.Logger,
	stream commandRequestStream,
	requests chan<- *pb.CommandRequest,
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

		select {
		case requests <- req:
		case <-ctx.Done():
			// Either the process is shutting down, or sending a response failed and the stream is broken. In both cases
			// the request cannot be answered any more.
			logger.Warn(
				"dropping a command request, it cannot be answered any more",
				"requestId", req.GetRequestId(),
			)
			return nil
		}
	}
}
