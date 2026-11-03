// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/parca-dev/parca/gen/proto/go/parca/debuginfo/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func main() {
	path := flag.String("config", "/etc/debuginfo-proxy/config.yaml", "configuration file")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx, *path, logger); err != nil {
		logger.Error("debuginfo-proxy stopped", "error", err)
		os.Exit(1)
	}
}

func newProxy(ctx context.Context, c *config) (*proxy, func(), error) {
	if err := c.validate(); err != nil {
		return nil, nil, err
	}
	p := &proxy{
		slots: make(chan struct{}, defaultMaxConcurrency), now: time.Now,
	}
	var connections []*grpc.ClientConn
	closeConnections := func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}
	for _, id := range c.Service.Pipelines["debuginfo"].Exporters {
		e := c.Exporters[id]
		tlsConfig, err := e.TLS.LoadTLSConfig(ctx)
		if err != nil {
			closeConnections()
			return nil, nil, fmt.Errorf("cannot load TLS configuration for exporter %s", id)
		}
		transport := insecure.NewCredentials()
		if tlsConfig != nil {
			transport = credentials.NewTLS(tlsConfig)
		}
		conn, err := grpc.NewClient(e.Endpoint, grpc.WithTransportCredentials(transport))
		if err != nil {
			closeConnections()
			return nil, nil, fmt.Errorf("cannot create client for exporter %s", id)
		}
		connections = append(connections, conn)
		p.destinations = append(p.destinations, destination{
			id: id, identity: destinationIdentity(e), client: pb.NewDebuginfoServiceClient(conn),
			headers: metadata.New(e.Headers),
		})
	}
	return p, closeConnections, nil
}

func run(ctx context.Context, path string, logger *slog.Logger) error {
	c, err := loadConfig(ctx, path)
	if err != nil {
		return err
	}
	p, closeConnections, err := newProxy(ctx, c)
	if err != nil {
		return err
	}
	defer closeConnections()
	r := c.Receivers["debuginfo"]
	maxMessageSize := 64 * 1024
	if len(p.destinations) == 1 {
		maxMessageSize = 64 * 1024 * 1024
	}
	opts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxMessageSize), grpc.MaxConcurrentStreams(128)}
	if r.GRPC.TLS != nil {
		tlsConfig, err := r.GRPC.TLS.LoadTLSConfig(ctx)
		if err != nil {
			return errors.New("cannot load receiver TLS configuration")
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	} else {
		logger.Warn("receiver TLS is disabled; use only behind a trusted TLS terminator or for local development")
	}
	server := grpc.NewServer(opts...)
	pb.RegisterDebuginfoServiceServer(server, p)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	listener, err := net.Listen("tcp", r.GRPC.Endpoint)
	if err != nil {
		return fmt.Errorf("cannot listen on debuginfo receiver: %w", err)
	}
	defer func() { _ = listener.Close() }()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			healthServer.Shutdown()
			timer := time.AfterFunc(defaultTimeout, server.Stop)
			defer timer.Stop()
			server.GracefulStop()
		case <-stopped:
		}
	}()
	logger.Info("debuginfo-proxy listening", "exporters", len(p.destinations))
	return server.Serve(listener)
}
