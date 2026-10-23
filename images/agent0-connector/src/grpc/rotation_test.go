// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

func TestIsRoutineRotation(t *testing.T) {
	maxAge := status.Error(
		codes.Unavailable,
		`closing transport due to: connection error: desc = "error reading from server: EOF", `+
			`received prior goaway: code: NO_ERROR, debug data: "max_age"`,
	)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "no error", err: nil, want: false},
		{name: "an error that is not a gRPC status", err: errors.New("boom"), want: false},
		{name: "a GOAWAY without an error code", err: maxAge, want: true},
		{name: "a wrapped GOAWAY without an error code", err: fmt.Errorf("stream receive failed: %w", maxAge), want: true},
		{
			name: "a GOAWAY with an error code",
			err: status.Error(
				codes.Unavailable,
				`closing transport due to: connection error: desc = "error reading from server: EOF", `+
					`received prior goaway: code: ENHANCE_YOUR_CALM, debug data: "too_many_pings"`,
			),
			want: false,
		},
		{
			name: "a connection that broke without a GOAWAY",
			err:  status.Error(codes.Unavailable, `error reading from server: EOF`),
			want: false,
		},
		{
			name: "another status code",
			err:  status.Error(codes.Aborted, "client ID taken over by a newer connection"),
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isRoutineRotation(test.err); got != test.want {
				t.Errorf("isRoutineRotation(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

func TestPlanReconnect(t *testing.T) {
	t.Run("reconnects promptly after a rotation and starts the backoff over", func(t *testing.T) {
		for range 1000 {
			wait, next := planReconnect(true, time.Second, maxReconnectDelay)
			if wait < 0 || wait >= maxRotationReconnectDelay {
				t.Fatalf("wait %s is outside the expected range [0, %s)", wait, maxRotationReconnectDelay)
			}
			if next != initialReconnectDelay {
				t.Fatalf("expected the next delay to be %s, got %s", initialReconnectDelay, next)
			}
		}
	})

	t.Run("resets the backoff after a healthy stream", func(t *testing.T) {
		wait, next := planReconnect(false, healthyStreamThreshold, maxReconnectDelay)
		if wait < initialReconnectDelay-initialReconnectDelay/5 || wait > initialReconnectDelay {
			t.Errorf("wait %s is not the jittered initial delay", wait)
		}
		if next != 2*initialReconnectDelay {
			t.Errorf("expected the next delay to be %s, got %s", 2*initialReconnectDelay, next)
		}
	})

	t.Run("backs off after a stream that was not healthy", func(t *testing.T) {
		current := 4 * time.Second
		wait, next := planReconnect(false, time.Second, current)
		if wait < current-current/5 || wait > current {
			t.Errorf("wait %s is not the jittered current delay", wait)
		}
		if next != 2*current {
			t.Errorf("expected the next delay to be %s, got %s", 2*current, next)
		}
	})
}

// TestRunStreamReportsBackendRotationAsRoutine checks against a real gRPC server that the way grpc-go reports a
// connection rotation still matches what isRoutineRotation looks for.
func TestRunStreamReportsBackendRotationAsRoutine(t *testing.T) {
	t.Run("when the connection reaches its maximum age", func(t *testing.T) {
		server, subscriber, _ := startRotationTestServer(t, grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      200 * time.Millisecond,
			MaxConnectionAgeGrace: 200 * time.Millisecond,
		}))
		defer server.Stop()

		err := runStreamWithTimeout(t, subscriber)
		if !isRoutineRotation(err) {
			t.Errorf("expected a routine rotation, got %v", err)
		}
	})

	t.Run("when the server shuts down gracefully", func(t *testing.T) {
		server, subscriber, subscribed := startRotationTestServer(t)
		defer server.Stop()
		go func() {
			<-subscribed
			go server.GracefulStop()
			// Give the GOAWAY time to reach the client, then end the stream that GracefulStop would wait for forever.
			time.Sleep(200 * time.Millisecond)
			server.Stop()
		}()

		err := runStreamWithTimeout(t, subscriber)
		if !isRoutineRotation(err) {
			t.Errorf("expected a routine rotation, got %v", err)
		}
	})

	t.Run("not when the server fails the stream", func(t *testing.T) {
		server, subscriber, _ := startRotationTestServerWithHandler(
			t,
			func(pb.OutboundConnectorService_SubscribeToCommandRequestsServer) error {
				return status.Error(codes.Unavailable, "backend unavailable")
			},
		)
		defer server.Stop()

		err := runStreamWithTimeout(t, subscriber)
		if err == nil || isRoutineRotation(err) {
			t.Errorf("expected an error that is not a routine rotation, got %v", err)
		}
	})
}

// rotationTestServer keeps every stream open until the server closes it, unless a handler replaces that behavior.
type rotationTestServer struct {
	pb.UnimplementedOutboundConnectorServiceServer
	subscribed chan struct{}
	handler    func(pb.OutboundConnectorService_SubscribeToCommandRequestsServer) error
}

func (s *rotationTestServer) SubscribeToCommandRequests(
	stream pb.OutboundConnectorService_SubscribeToCommandRequestsServer,
) error {
	if s.handler != nil {
		return s.handler(stream)
	}
	close(s.subscribed)
	<-stream.Context().Done()
	return nil
}

func startRotationTestServer(t *testing.T, opts ...grpc.ServerOption) (*grpc.Server, *Subscriber, <-chan struct{}) {
	t.Helper()
	return startRotationTestServerWithHandler(t, nil, opts...)
}

func startRotationTestServerWithHandler(
	t *testing.T,
	streamHandler func(pb.OutboundConnectorService_SubscribeToCommandRequestsServer) error,
	opts ...grpc.ServerOption,
) (*grpc.Server, *Subscriber, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	server := grpc.NewServer(opts...)
	handler := &rotationTestServer{subscribed: make(chan struct{}), handler: streamHandler}
	pb.RegisterOutboundConnectorServiceServer(server, handler)
	go func() {
		_ = server.Serve(listener)
	}()
	subscriber := &Subscriber{
		serverAddress:         listener.Addr().String(),
		transportCredentials:  insecure.NewCredentials(),
		clientID:              "cluster-uid",
		authToken:             "auth-token",
		maxConcurrentCommands: 1,
	}
	return server, subscriber, handler.subscribed
}

func runStreamWithTimeout(t *testing.T, subscriber *Subscriber) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := subscriber.runStream(ctx, discardLogger())
	if ctx.Err() != nil {
		t.Fatalf("the stream did not end before the timeout")
	}
	return err
}
