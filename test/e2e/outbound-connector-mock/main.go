// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// outbound-connector-mock is a minimal gRPC server that implements
// outboundconnector.OutboundConnectorService. It stands in for the Dash0 backend's outbound-connector component in the
// operator's e2e tests: the agent0-connector workload opens the SubscribeToCommandRequests bidirectional stream against
// it, and the test asserts that the connection was established with the expected gRPC metadata.
//
// The mock keeps track of which clients are connected (including the gRPC metadata they announced themselves with) and
// records every CommandResponse it receives. It exposes a separate HTTP control/debug port that the e2e test uses to:
//   - query which clients are currently connected (GET /clients),
//   - trigger sending a CommandRequest down a connected client's stream (POST /command-requests),
//   - query which CommandResponses have been received (GET /command-responses).
//
// It implements no real command routing or authentication logic; it only exists for wiring assertions in e2e tests.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	// Register the gzip encoding so the server can decompress streams from clients that enable gzip. The
	// agent0-connector does not currently enable compression, but registering the codec keeps the mock robust against
	// that changing without failing every RPC with Unimplemented "Decompressor is not installed".
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"

	pb "outbound-connector-mock/proto"
)

const (
	grpcAddr  = ":8022"
	debugAddr = ":8024"

	// metadataClientID is the gRPC metadata key via which a subscribing client announces its unique client ID. It
	// mirrors the key used by the agent0-connector (and the real outbound-connector backend).
	metadataClientID = "dash0-client-id"

	// metadataAuthorization is the gRPC metadata key via which a subscribing client sends its Dash0 authorization
	// token (prefixed with "Bearer ").
	metadataAuthorization = "authorization"

	// sendTimeout bounds how long triggering a command request waits for the target client's stream writer to pick the
	// request up, so a wedged stream cannot block the HTTP handler indefinitely.
	sendTimeout = 5 * time.Second
)

// connectedClient holds the state for a single client currently subscribed via SubscribeToCommandRequests.
type connectedClient struct {
	clientID      string
	authorization string
	connectedAt   time.Time

	// sendChan carries CommandRequests that should be pushed down this client's stream. It is consumed by the stream's
	// writer loop in SubscribeToCommandRequests.
	sendChan chan *pb.CommandRequest
}

// clientInfo is the JSON representation of a connected client returned via the HTTP debug API.
type clientInfo struct {
	ClientID      string    `json:"clientId"`
	Authorization string    `json:"authorization"`
	ConnectedAt   time.Time `json:"connectedAt"`
}

// commandResponse is the JSON representation of a received CommandResponse returned via the HTTP debug API.
type commandResponse struct {
	RequestID string `json:"requestId"`
	ExitCode  int32  `json:"exitCode"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Timeout   bool   `json:"timeout"`
}

// triggerCommandRequest is the JSON request body for POST /command-requests.
type triggerCommandRequest struct {
	ClientID  string   `json:"clientId"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}

// triggerCommandResponse is the JSON response body for POST /command-requests.
type triggerCommandResponse struct {
	RequestID string `json:"requestId"`
}

// state holds all server state shared between the gRPC handlers and the HTTP debug handlers.
type state struct {
	mu sync.Mutex

	// clients holds the currently connected clients, keyed by client ID. There is at most one entry per client ID; a
	// newer connection with the same client ID replaces the previous one.
	clients map[string]*connectedClient

	// responses accumulates every CommandResponse received from any client, in the order received.
	responses []commandResponse
}

func newState() *state {
	return &state{
		clients: make(map[string]*connectedClient),
	}
}

func (s *state) registerClient(c *connectedClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.clientID] = c
}

// unregisterClient removes the client, but only if the registry still points at this exact client (a newer connection
// with the same client ID may have taken over in the meantime).
func (s *state) unregisterClient(c *connectedClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.clients[c.clientID]; ok && current == c {
		delete(s.clients, c.clientID)
	}
}

func (s *state) listClients() []clientInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]clientInfo, 0, len(s.clients))
	for _, c := range s.clients {
		infos = append(infos, clientInfo{
			ClientID:      c.clientID,
			Authorization: c.authorization,
			ConnectedAt:   c.connectedAt,
		})
	}
	return infos
}

func (s *state) lookupClient(clientID string) *connectedClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients[clientID]
}

func (s *state) recordResponse(resp commandResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, resp)
}

func (s *state) listResponses() []commandResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	responses := make([]commandResponse, len(s.responses))
	copy(responses, s.responses)
	return responses
}

type outboundConnectorServer struct {
	pb.UnimplementedOutboundConnectorServiceServer
	state *state
}

// SubscribeToCommandRequests handles the bidirectional stream opened by a client (the agent0-connector). It records the
// client (and the gRPC metadata it announced itself with) so the e2e test can assert the connection was established
// correctly, forwards CommandRequests queued via the HTTP debug API down the stream, and stores every CommandResponse
// received back from the client.
func (s *outboundConnectorServer) SubscribeToCommandRequests(
	stream grpc.BidiStreamingServer[pb.CommandResponse, pb.CommandRequest],
) error {
	ctx := stream.Context()

	clientID, authorization := metadataFromContext(ctx)
	client := &connectedClient{
		clientID:      clientID,
		authorization: authorization,
		connectedAt:   time.Now().UTC(),
		sendChan:      make(chan *pb.CommandRequest, 16),
	}
	s.state.registerClient(client)
	defer s.state.unregisterClient(client)

	log.Printf(
		"client subscribed to command requests (client_id=%q, authorization=%q)",
		clientID,
		authorization,
	)

	// Reader goroutine: receive command responses from the client and record them.
	errChan := make(chan error, 1)
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					errChan <- nil
					return
				}
				errChan <- err
				return
			}
			s.state.recordResponse(commandResponse{
				RequestID: resp.GetRequestId(),
				ExitCode:  resp.GetExitCode(),
				Stdout:    resp.GetStdout(),
				Stderr:    resp.GetStderr(),
				Timeout:   resp.GetTimeout(),
			})
			log.Printf(
				"received command response (client_id=%q, request_id=%q, exit_code=%d)",
				clientID,
				resp.GetRequestId(),
				resp.GetExitCode(),
			)
		}
	}()

	// Writer loop: forward command requests queued via the HTTP debug API down the stream.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case req := <-client.sendChan:
			if err := stream.Send(req); err != nil {
				return err
			}
			log.Printf(
				"sent command request (client_id=%q, request_id=%q, command=%q, arguments=%v)",
				clientID,
				req.GetRequestId(),
				req.GetCommand(),
				req.GetArguments(),
			)
		}
	}
}

// metadataFromContext extracts the client ID and authorization values announced by the client via gRPC metadata.
func metadataFromContext(ctx context.Context) (clientID string, authorization string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	return firstMetadataValue(md, metadataClientID), firstMetadataValue(md, metadataAuthorization)
}

func firstMetadataValue(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}

func main() {
	st := newState()
	go runDebugServer(st)
	runGrpcServer(st)
}

func runGrpcServer(st *state) {
	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterOutboundConnectorServiceServer(grpcServer, &outboundConnectorServer{state: st})
	log.Printf("outbound-connector-mock gRPC server listening on %s", grpcAddr)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("gRPC server failed: %v", err)
	}
}

func runDebugServer(st *state) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/clients", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, st.listClients())
	})
	mux.HandleFunc("/command-responses", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, st.listResponses())
	})
	mux.HandleFunc("/command-requests", func(w http.ResponseWriter, r *http.Request) {
		handleTriggerCommandRequest(st, w, r)
	})

	server := &http.Server{Addr: debugAddr, Handler: mux}
	server.SetKeepAlivesEnabled(false)
	log.Printf("outbound-connector-mock debug HTTP server listening on %s", debugAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("debug HTTP server failed: %v", err)
	}
}

// handleTriggerCommandRequest pushes a CommandRequest down the stream of the client identified by the request body. The
// mock generates the request ID and returns it so the caller can correlate it with the eventual CommandResponse.
func handleTriggerCommandRequest(st *state, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body triggerCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ClientID == "" {
		http.Error(w, "clientId is required", http.StatusBadRequest)
		return
	}
	if body.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	client := st.lookupClient(body.ClientID)
	if client == nil {
		http.Error(w, "no client connected for clientId "+body.ClientID, http.StatusNotFound)
		return
	}

	req := &pb.CommandRequest{
		RequestId: uuid.NewString(),
		Command:   body.Command,
		Arguments: body.Arguments,
	}

	select {
	case client.sendChan <- req:
		writeJSON(w, triggerCommandResponse{RequestID: req.RequestId})
	case <-time.After(sendTimeout):
		http.Error(w, "timed out queueing command request for clientId "+body.ClientID, http.StatusServiceUnavailable)
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
