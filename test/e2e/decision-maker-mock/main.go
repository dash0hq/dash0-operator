// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// decision-maker-mock is a minimal gRPC server that implements
// decisionmaker.DecisionMakerService. It is used by the operator's e2e tests
// to keep the Barker proxy connected to a fake upstream Decision Maker so
// that Barker pod readiness and connection wiring can be asserted. It does
// not implement any decision-making logic; it only accepts streams and
// counts observed RPCs, exposing those counts on a separate HTTP debug port.

package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"

	"google.golang.org/grpc"
	// Register the gzip encoding so the server can decompress streams from
	// clients that enable gzip (Barker's decision-maker-client does, see
	// `gRPC compression enabled` in its startup log). Without this, every
	// RPC fails with Unimplemented "Decompressor is not installed".
	_ "google.golang.org/grpc/encoding/gzip"

	pb "decision-maker-mock/proto"
)

const (
	grpcAddr  = ":8011"
	debugAddr = ":8101"
)

type rpcCounters struct {
	decisionStream         atomic.Int64
	subscribeServerInfo    atomic.Int64
	subscribeSamplingRules atomic.Int64
}

func (c *rpcCounters) snapshot() map[string]int64 {
	return map[string]int64{
		"DecisionStream":         c.decisionStream.Load(),
		"SubscribeServerInfo":    c.subscribeServerInfo.Load(),
		"SubscribeSamplingRules": c.subscribeSamplingRules.Load(),
	}
}

type decisionMakerServer struct {
	pb.UnimplementedDecisionMakerServiceServer
	counters *rpcCounters
}

// DecisionStream reads incoming TraceReports from the client and never sends
// TraceDecisions back. Barker keeps the stream open and forwards reports;
// not returning decisions is fine for wiring tests.
func (s *decisionMakerServer) DecisionStream(stream pb.DecisionMakerService_DecisionStreamServer) error {
	s.counters.decisionStream.Add(1)
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// SubscribeServerInfo emits a single response with a stable instance_id and
// then blocks until the client disconnects.
func (s *decisionMakerServer) SubscribeServerInfo(
	_ *pb.SubscribeServerInfoRequest,
	stream pb.DecisionMakerService_SubscribeServerInfoServer,
) error {
	s.counters.subscribeServerInfo.Add(1)
	if err := stream.Send(&pb.SubscribeServerInfoResponse{
		NumberOfDecisionMakers: 1,
		InstanceId:             "decision-maker-mock",
	}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

// SubscribeSamplingRules emits one empty rules response and blocks until the
// client disconnects.
func (s *decisionMakerServer) SubscribeSamplingRules(
	_ *pb.SubscribeSamplingRulesRequest,
	stream pb.DecisionMakerService_SubscribeSamplingRulesServer,
) error {
	s.counters.subscribeSamplingRules.Add(1)
	if err := stream.Send(&pb.SubscribeSamplingRulesResponse{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func main() {
	counters := &rpcCounters{}

	go runDebugServer(counters)
	runGrpcServer(counters)
}

func runGrpcServer(counters *rpcCounters) {
	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterDecisionMakerServiceServer(grpcServer, &decisionMakerServer{counters: counters})
	log.Printf("decision-maker-mock gRPC server listening on %s", grpcAddr)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("gRPC server failed: %v", err)
	}
}

func runDebugServer(counters *rpcCounters) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/grpc-calls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(counters.snapshot())
	})
	mux.HandleFunc("/grpc-calls/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counters.decisionStream.Store(0)
		counters.subscribeServerInfo.Store(0)
		counters.subscribeSamplingRules.Store(0)
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Addr: debugAddr, Handler: mux}
	server.SetKeepAlivesEnabled(false)
	log.Printf("decision-maker-mock debug HTTP server listening on %s", debugAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("debug HTTP server failed: %v", err)
	}
}
