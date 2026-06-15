// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

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
	t.Setenv(kubectlTmpEnvVarName, "/var/cache/kubectl")

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

	idx  int
	sent []*pb.CommandResponse
}

func (f *fakeStream) Recv() (*pb.CommandRequest, error) {
	if f.idx < len(f.requests) {
		req := f.requests[f.idx]
		f.idx++
		return req, nil
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
	f.sent = append(f.sent, resp)
	return nil
}

func TestListenToCommandRequests(t *testing.T) {
	logger := discardLogger()

	t.Run("executes a request and sends back the response, then returns nil on a clean close", func(t *testing.T) {
		// A non-kubectl command is rejected by validation, so no kubectl binary is needed for this test.
		stream := &fakeStream{requests: []*pb.CommandRequest{
			{RequestId: "req-1", Command: "not-kubectl", Arguments: []string{"get"}},
		}}

		if err := listenToCommandRequests(context.Background(), logger, stream, "/tmp"); err != nil {
			t.Fatalf("expected a nil error on clean stream close, got %v", err)
		}
		if len(stream.sent) != 1 {
			t.Fatalf("expected exactly one response to be sent, got %d", len(stream.sent))
		}
		if stream.sent[0].GetRequestId() != "req-1" {
			t.Errorf("expected the response to echo the request ID, got %q", stream.sent[0].GetRequestId())
		}
		if stream.sent[0].GetExitCode() != exitCodeRejected {
			t.Errorf("expected the rejected exit code %d, got %d", exitCodeRejected, stream.sent[0].GetExitCode())
		}
	})

	t.Run("returns a wrapped error on a non-EOF receive failure", func(t *testing.T) {
		recvErr := errors.New("connection reset")
		stream := &fakeStream{recvErr: recvErr}

		err := listenToCommandRequests(context.Background(), logger, stream, "/tmp")
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

		err := listenToCommandRequests(context.Background(), logger, stream, "/tmp")
		if err == nil {
			t.Fatal("expected an error on a send failure, got nil")
		}
		if !errors.Is(err, sendErr) {
			t.Errorf("expected the send error to be wrapped, got %v", err)
		}
	})
}
