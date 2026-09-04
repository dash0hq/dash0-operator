// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dash0hq/dash0-operator/images/agent0-connector/kubectl"
	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestResolveAuthToken(t *testing.T) {
	logger := discardLogger()
	t.Setenv(authTokenEnvVarName, "agent0-connector-auth-token")

	token := resolveAuthToken(logger)

	if token != "agent0-connector-auth-token" {
		t.Errorf("expected the auth token to be read from the environment variable, got %q", token)
	}
}

func TestResolveServerAddress(t *testing.T) {
	logger := discardLogger()

	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "keeps a plain host and port",
			value:    "outbound-connector.eu-west-1.aws.dash0.com:4317",
			expected: "outbound-connector.eu-west-1.aws.dash0.com:4317",
		},
		{
			name:     "removes an https prefix",
			value:    "https://outbound-connector.eu-west-1.aws.dash0.com:4317",
			expected: "outbound-connector.eu-west-1.aws.dash0.com:4317",
		},
		{
			name:     "removes an http prefix",
			value:    "http://host.docker.internal:8022",
			expected: "host.docker.internal:8022",
		},
		{
			name:     "removes a prefix written in upper case",
			value:    "HTTPS://outbound-connector.eu-west-1.aws.dash0.com",
			expected: "outbound-connector.eu-west-1.aws.dash0.com",
		},
		{
			name:     "removes a trailing slash",
			value:    "https://outbound-connector.eu-west-1.aws.dash0.com:443/",
			expected: "outbound-connector.eu-west-1.aws.dash0.com:443",
		},
		{
			name:     "removes a path, a query and a fragment",
			value:    "https://outbound-connector.eu-west-1.aws.dash0.com:443/v1/commands?foo=bar#baz",
			expected: "outbound-connector.eu-west-1.aws.dash0.com:443",
		},
		{
			name:     "keeps a gRPC target with a scheme of its own",
			value:    "dns:///outbound-connector.eu-west-1.aws.dash0.com:443",
			expected: "dns:///outbound-connector.eu-west-1.aws.dash0.com:443",
		},
		{
			name:     "keeps a host that merely starts with the letters of a prefix",
			value:    "https-endpoint.dash0.com:443",
			expected: "https-endpoint.dash0.com:443",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(serverAddressEnvVarName, test.value)
			if serverAddress := resolveServerAddress(logger); serverAddress != test.expected {
				t.Errorf("expected the server address %q to resolve to %q, got %q", test.value, test.expected, serverAddress)
			}
		})
	}
}

func TestResolveClientID(t *testing.T) {
	logger := discardLogger()

	t.Run("uses the cluster UID environment variable when set", func(t *testing.T) {
		t.Setenv(clusterUidEnvVarName, "test-cluster-uid")
		if clientID := resolveClientID(logger); clientID != "test-cluster-uid" {
			t.Errorf("expected the client ID to be the cluster UID, got %q", clientID)
		}
	})
}

func TestResolveKubectlTmpDir(t *testing.T) {
	logger := discardLogger()
	t.Setenv(kubectl.KubectlTmpEnvVarName, "/var/cache/kubectl")

	if tmpDir := resolveKubectlTmpDir(logger); tmpDir != "/var/cache/kubectl" {
		t.Errorf("expected the tmp dir to be read from the environment variable, got %q", tmpDir)
	}
}

func TestResolveTransportCredentials(t *testing.T) {
	logger := discardLogger()

	t.Run("uses TLS by default", func(t *testing.T) {
		t.Setenv(insecureEnvVarName, "")
		creds := resolveTransportCredentials(logger)
		if proto := creds.Info().SecurityProtocol; proto != "tls" {
			t.Errorf("expected TLS transport credentials, got security protocol %q", proto)
		}
	})

	t.Run("uses plaintext when the insecure flag is set", func(t *testing.T) {
		t.Setenv(insecureEnvVarName, "true")
		creds := resolveTransportCredentials(logger)
		if proto := creds.Info().SecurityProtocol; proto != "insecure" {
			t.Errorf("expected insecure transport credentials, got security protocol %q", proto)
		}
	})
}

func TestNextReconnectDelay(t *testing.T) {
	if got := nextReconnectDelay(initialReconnectDelay); got != 2*initialReconnectDelay {
		t.Errorf("expected the delay to double, got %s", got)
	}
	if got := nextReconnectDelay(maxReconnectDelay); got != maxReconnectDelay {
		t.Errorf("expected the delay to be capped at %s, got %s", maxReconnectDelay, got)
	}
	if got := nextReconnectDelay(maxReconnectDelay / 2); got != maxReconnectDelay {
		t.Errorf("expected doubling past the cap to clamp to %s, got %s", maxReconnectDelay, got)
	}
}

func TestJitter(t *testing.T) {
	const d = 10 * time.Second
	for range 1000 {
		got := jitter(d)
		if got < d-d/5 || got > d {
			t.Fatalf("jittered delay %s is outside the expected range [%s, %s]", got, d-d/5, d)
		}
	}
	// A delay too small to jitter is returned unchanged.
	if got := jitter(2); got != 2 {
		t.Errorf("expected a sub-divisible delay to be returned unchanged, got %s", got)
	}
}

// fakeStream is an in-memory commandRequestStream for testing listenToCommandRequests. It yields the queued requests in
// order, then returns recvErr (defaulting to io.EOF), and records every response sent back.
type fakeStream struct {
	requests []*pb.CommandRequest
	recvErr  error
	sendErr  error

	// recvGate, when set, holds Recv once all queued requests have been yielded, until the channel is closed. This
	// keeps the receive loop running while a test inspects what has been sent so far.
	recvGate chan struct{}

	// sentSignal, when set, receives every response that was sent, so that a test can wait for one instead of polling.
	// It has to be buffered for as many responses as the test expects.
	sentSignal chan *pb.CommandResponse

	// idx is only accessed by Recv, which listenToCommandRequests calls from a single goroutine.
	idx int

	mu   sync.Mutex
	sent []*pb.CommandResponse
}

func (f *fakeStream) Recv() (*pb.CommandRequest, error) {
	if f.idx < len(f.requests) {
		req := f.requests[f.idx]
		f.idx++
		return req, nil
	}
	if f.recvGate != nil {
		<-f.recvGate
	}
	if f.recvErr != nil {
		return nil, f.recvErr
	}
	return nil, io.EOF
}

func (f *fakeStream) Send(resp *pb.CommandResponse) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.mu.Lock()
	f.sent = append(f.sent, resp)
	f.mu.Unlock()
	if f.sentSignal != nil {
		f.sentSignal <- resp
	}
	return nil
}

// sentResponses returns a copy of the responses sent so far, safe to read while the sender is still running.
func (f *fakeStream) sentResponses() []*pb.CommandResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

// awaitSend returns the next response that is sent, and fails the test if none is sent in time.
func (f *fakeStream) awaitSend(t *testing.T) *pb.CommandResponse {
	t.Helper()
	select {
	case resp := <-f.sentSignal:
		return resp
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a response to be sent")
		return nil
	}
}

// testTimeout bounds every wait in the concurrency tests, so that a hang fails the test instead of the suite.
const testTimeout = 10 * time.Second

// awaitListenResult returns the error listenToCommandRequests returned, and fails the test if it does not return in
// time.
func awaitListenResult(t *testing.T, listenDone <-chan error) error {
	t.Helper()
	select {
	case err := <-listenDone:
		return err
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for listenToCommandRequests to return")
		return nil
	}
}

// listen runs listenToCommandRequests and returns its error, failing the test if it does not return within testTimeout
// rather than hanging the whole suite on a deadlock.
func listen(
	t *testing.T,
	logger *slog.Logger,
	stream commandRequestStream,
	maxConcurrentCommands int,
	execute commandExecutor,
) error {
	t.Helper()
	listenDone := startListening(logger, stream, maxConcurrentCommands, execute)
	return awaitListenResult(t, listenDone)
}

// startListening runs listenToCommandRequests in the background and returns the channel on which it reports its error,
// for the tests that have to interact with the stream while it is running.
func startListening(
	logger *slog.Logger,
	stream commandRequestStream,
	maxConcurrentCommands int,
	execute commandExecutor,
) <-chan error {
	return startListeningWithContext(context.Background(), logger, stream, maxConcurrentCommands, execute)
}

// startListeningWithContext is startListening for the tests that have to cancel the context of
// listenToCommandRequests.
func startListeningWithContext(
	ctx context.Context,
	logger *slog.Logger,
	stream commandRequestStream,
	maxConcurrentCommands int,
	execute commandExecutor,
) <-chan error {
	listenDone := make(chan error, 1)
	go func() {
		listenDone <- listenToCommandRequests(
			ctx,
			logger,
			stream,
			"/tmp",
			maxConcurrentCommands,
			execute,
		)
	}()
	return listenDone
}

// requestIDs returns the request IDs of the given responses, sorted, so that a test can assert on them without
// depending on the order in which the workers finished.
func requestIDs(responses []*pb.CommandResponse) []string {
	ids := make([]string, 0, len(responses))
	for _, resp := range responses {
		ids = append(ids, resp.GetRequestId())
	}
	slices.Sort(ids)
	return ids
}

func TestListenToCommandRequests(t *testing.T) {
	logger := discardLogger()

	t.Run("executes a request and sends back the response, then returns nil on a clean close", func(t *testing.T) {
		// A non-kubectl command is rejected by validation, so no kubectl binary is needed for this test.
		stream := &fakeStream{requests: []*pb.CommandRequest{
			{RequestId: "req-1", Command: "not-kubectl", Arguments: []string{"get"}},
		}}

		if err := listen(t, logger, stream, defaultMaxConcurrentCommands, kubectl.ExecuteCommandRequest); err != nil {
			t.Fatalf("expected a nil error on clean stream close, got %v", err)
		}
		sent := stream.sentResponses()
		if len(sent) != 1 {
			t.Fatalf("expected exactly one response to be sent, got %d", len(sent))
		}
		if sent[0].GetRequestId() != "req-1" {
			t.Errorf("expected the response to echo the request ID, got %q", sent[0].GetRequestId())
		}
		if sent[0].GetExitCode() != 1 {
			t.Errorf("expected the rejected exit code %d, got %d", 1, sent[0].GetExitCode())
		}
	})

	t.Run("returns a wrapped error on a non-EOF receive failure", func(t *testing.T) {
		recvErr := errors.New("connection reset")
		stream := &fakeStream{recvErr: recvErr}

		err := listen(t, logger, stream, defaultMaxConcurrentCommands, kubectl.ExecuteCommandRequest)
		if err == nil {
			t.Fatal("expected an error on a receive failure, got nil")
		}
		if !errors.Is(err, recvErr) {
			t.Errorf("expected the receive error to be wrapped, got %v", err)
		}
	})

	t.Run("returns a wrapped error when sending the response fails", func(t *testing.T) {
		sendErr := errors.New("broken pipe")
		stream := &fakeStream{
			requests: []*pb.CommandRequest{{RequestId: "req-1", Command: "not-kubectl"}},
			sendErr:  sendErr,
		}

		err := listen(t, logger, stream, defaultMaxConcurrentCommands, kubectl.ExecuteCommandRequest)
		if err == nil {
			t.Fatal("expected an error on a send failure, got nil")
		}
		if !errors.Is(err, sendErr) {
			t.Errorf("expected the send error to be wrapped, got %v", err)
		}
	})

	t.Run("answers the requests behind a slow command while it is still running", func(t *testing.T) {
		slowStarted := make(chan struct{})
		releaseSlow := make(chan struct{})
		recvGate := make(chan struct{})
		stream := &fakeStream{
			requests: []*pb.CommandRequest{
				{RequestId: "slow-1", Command: "kubectl", Arguments: []string{"get", "pods", "-A"}},
				{RequestId: "fast-2", Command: "kubectl", Arguments: []string{"version"}},
			},
			recvGate:   recvGate,
			sentSignal: make(chan *pb.CommandResponse, 2),
		}
		execute := func(
			ctx context.Context,
			_ *slog.Logger,
			_ string,
			req *pb.CommandRequest,
		) *pb.CommandResponse {
			if req.GetRequestId() == "slow-1" {
				close(slowStarted)
				select {
				case <-releaseSlow:
				case <-ctx.Done():
				}
			}
			return &pb.CommandResponse{RequestId: req.GetRequestId()}
		}

		listenDone := startListening(logger, stream, 2, execute)

		<-slowStarted
		// The slow command still occupies its worker, so this response can only have been produced concurrently.
		if resp := stream.awaitSend(t); resp.GetRequestId() != "fast-2" {
			t.Errorf("expected the fast request to be answered first, got %q", resp.GetRequestId())
		}

		close(releaseSlow)
		close(recvGate)
		if err := awaitListenResult(t, listenDone); err != nil {
			t.Fatalf("expected a nil error on clean stream close, got %v", err)
		}
		if ids := requestIDs(stream.sentResponses()); !slices.Equal(ids, []string{"fast-2", "slow-1"}) {
			t.Errorf("expected both requests to be answered, got %v", ids)
		}
	})

	t.Run("executes at most maxConcurrentCommands commands at the same time", func(t *testing.T) {
		const maxConcurrentCommands = 2
		const numberOfRequests = maxConcurrentCommands + 3

		requests := make([]*pb.CommandRequest, 0, numberOfRequests)
		for i := range numberOfRequests {
			requests = append(requests, &pb.CommandRequest{
				RequestId: fmt.Sprintf("req-%d", i),
				Command:   "kubectl",
				Arguments: []string{"version"},
			})
		}
		recvGate := make(chan struct{})
		stream := &fakeStream{
			requests:   requests,
			recvGate:   recvGate,
			sentSignal: make(chan *pb.CommandResponse, numberOfRequests),
		}

		release := make(chan struct{})
		started := make(chan struct{}, numberOfRequests)
		var running atomic.Int32
		var peak atomic.Int32
		execute := func(
			_ context.Context,
			_ *slog.Logger,
			_ string,
			req *pb.CommandRequest,
		) *pb.CommandResponse {
			current := running.Add(1)
			for {
				observed := peak.Load()
				if current <= observed || peak.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			running.Add(-1)
			return &pb.CommandResponse{RequestId: req.GetRequestId()}
		}

		listenDone := startListening(logger, stream, maxConcurrentCommands, execute)

		for range maxConcurrentCommands {
			select {
			case <-started:
			case <-time.After(testTimeout):
				t.Fatal("timed out waiting for the workers to start executing")
			}
		}
		// Every worker is now blocked in execute. Give the pool the chance to start a command it must not start.
		time.Sleep(100 * time.Millisecond)
		if observed := peak.Load(); observed != maxConcurrentCommands {
			t.Errorf("expected exactly %d commands to run at the same time, got %d", maxConcurrentCommands, observed)
		}

		close(release)
		close(recvGate)
		if err := awaitListenResult(t, listenDone); err != nil {
			t.Fatalf("expected a nil error on clean stream close, got %v", err)
		}
		if sent := stream.sentResponses(); len(sent) != numberOfRequests {
			t.Errorf("expected all %d requests to be answered, got %d", numberOfRequests, len(sent))
		}
		if observed := peak.Load(); observed > maxConcurrentCommands {
			t.Errorf("expected at most %d commands to run at the same time, got %d", maxConcurrentCommands, observed)
		}
	})
}

// TestListenToCommandRequestsAbortsRunningCommands covers the two paths that abort the commands which are still
// running, because their responses cannot be delivered any more.
func TestListenToCommandRequestsAbortsRunningCommands(t *testing.T) {
	logger := discardLogger()

	t.Run("aborts the running commands when sending a response fails", func(t *testing.T) {
		sendErr := errors.New("broken pipe")
		blockedStarted := make(chan struct{})
		blockedCancelled := make(chan struct{})
		stream := &fakeStream{
			requests: []*pb.CommandRequest{
				{RequestId: "blocked-1", Command: "kubectl", Arguments: []string{"get", "pods", "-A"}},
				{RequestId: "quick-2", Command: "kubectl", Arguments: []string{"version"}},
			},
			sendErr: sendErr,
		}
		execute := func(
			ctx context.Context,
			_ *slog.Logger,
			_ string,
			req *pb.CommandRequest,
		) *pb.CommandResponse {
			if req.GetRequestId() == "blocked-1" {
				close(blockedStarted)
				<-ctx.Done()
				close(blockedCancelled)
			} else {
				// Hand the response over only once the other worker is parked, so that the send failure happens while
				// a command is still running.
				<-blockedStarted
			}
			return &pb.CommandResponse{RequestId: req.GetRequestId()}
		}

		listenDone := startListening(logger, stream, 2, execute)

		select {
		case <-blockedCancelled:
		case <-time.After(testTimeout):
			t.Fatal("timed out waiting for the running command to be aborted after the send failure")
		}
		err := awaitListenResult(t, listenDone)
		if err == nil {
			t.Fatal("expected an error on a send failure, got nil")
		}
		if !errors.Is(err, sendErr) {
			t.Errorf("expected the send error to be wrapped, got %v", err)
		}
	})

	t.Run("aborts the running commands when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		longStarted := make(chan struct{})
		longCancelled := make(chan struct{})
		stream := &fakeStream{requests: []*pb.CommandRequest{
			{RequestId: "long-1", Command: "kubectl", Arguments: []string{"get", "pods", "-A"}},
		}}
		execute := func(
			execCtx context.Context,
			_ *slog.Logger,
			_ string,
			req *pb.CommandRequest,
		) *pb.CommandResponse {
			close(longStarted)
			<-execCtx.Done()
			close(longCancelled)
			return &pb.CommandResponse{RequestId: req.GetRequestId()}
		}

		listenDone := startListeningWithContext(ctx, logger, stream, defaultMaxConcurrentCommands, execute)

		select {
		case <-longStarted:
		case <-time.After(testTimeout):
			t.Fatal("timed out waiting for the command to start executing")
		}
		cancel()

		select {
		case <-longCancelled:
		case <-time.After(testTimeout):
			t.Fatal("timed out waiting for the running command to be aborted after the context was cancelled")
		}
		if err := awaitListenResult(t, listenDone); err != nil {
			t.Fatalf("expected a nil error on clean stream close, got %v", err)
		}
	})
}

func TestResolveMaxConcurrentCommands(t *testing.T) {
	logger := discardLogger()

	t.Run("uses the value from the environment", func(t *testing.T) {
		t.Setenv(maxConcurrentCommandsEnvVarName, "7")
		if got := resolveMaxConcurrentCommands(logger); got != 7 {
			t.Errorf("expected the value from the environment variable, got %d", got)
		}
	})

	t.Run("falls back to the default", func(t *testing.T) {
		for _, value := range []string{"", "0", "-1", "not-a-number", "2.5"} {
			t.Setenv(maxConcurrentCommandsEnvVarName, value)
			if got := resolveMaxConcurrentCommands(logger); got != defaultMaxConcurrentCommands {
				t.Errorf("expected the default %d for the value %q, got %d", defaultMaxConcurrentCommands, value, got)
			}
		}
	})
}
