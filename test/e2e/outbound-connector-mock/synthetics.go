// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "outbound-connector-mock/proto"
)

const (
	// httpCheckApiVersion is the apiVersion of the HttpRequestParams/HttpRequestResult JSON payloads the
	// synthetics-worker supports.
	httpCheckApiVersion = "dash0.com/v1alpha1"

	httpCheckTaskTimeout = 30 * time.Second
	httpCheckTimeout     = "10s"
)

// syntheticsWorker holds the state for a single synthetics-worker currently subscribed via
// SyntheticTaskService/Subscribe. All fields except sendChan are guarded by state.mu.
type syntheticsWorker struct {
	workerID      string
	locationID    string
	workerVersion string
	authorization string
	connectedAt   time.Time
	heartbeats    int
	lastHeartbeat time.Time

	// sendChan carries TaskRequests that should be pushed down this worker's stream. It is consumed by the stream's
	// writer loop in Subscribe.
	sendChan chan *pb.TaskRequest
}

// syntheticsWorkerInfo is the JSON representation of a connected synthetics-worker returned via the HTTP debug API.
type syntheticsWorkerInfo struct {
	WorkerID      string    `json:"workerId"`
	LocationID    string    `json:"locationId"`
	WorkerVersion string    `json:"workerVersion"`
	Authorization string    `json:"authorization"`
	ConnectedAt   time.Time `json:"connectedAt"`
	Heartbeats    int       `json:"heartbeats"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}

// syntheticsTaskResult is the JSON representation of a received TaskResult returned via the HTTP debug API. For an
// executed HTTP check, Result holds the JSON-encoded syntheticchecks.HttpRequestResult; for a task the worker could not
// execute, FailureReason and FailureMessage are set instead.
type syntheticsTaskResult struct {
	TaskID         string          `json:"taskId"`
	WorkerID       string          `json:"workerId"`
	ApiVersion     string          `json:"apiVersion,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	FailureReason  string          `json:"failureReason,omitempty"`
	FailureMessage string          `json:"failureMessage,omitempty"`
}

// triggerSyntheticsTaskRequest is the JSON request body for POST /synthetics-task-requests.
type triggerSyntheticsTaskRequest struct {
	LocationID string `json:"locationId"`
	Url        string `json:"url"`
}

// triggerSyntheticsTaskResponse is the JSON response body for POST /synthetics-task-requests.
type triggerSyntheticsTaskResponse struct {
	TaskID   string `json:"taskId"`
	WorkerID string `json:"workerId"`
}

func (s *state) registerSyntheticsWorker(w *syntheticsWorker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syntheticsWorkers[w.workerID] = w
}

func (s *state) unregisterSyntheticsWorker(w *syntheticsWorker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.syntheticsWorkers[w.workerID]; ok && current == w {
		delete(s.syntheticsWorkers, w.workerID)
	}
}

func (s *state) recordSyntheticsHeartbeat(w *syntheticsWorker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.heartbeats++
	w.lastHeartbeat = time.Now().UTC()
}

func (s *state) listSyntheticsWorkers() []syntheticsWorkerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]syntheticsWorkerInfo, 0, len(s.syntheticsWorkers))
	for _, w := range s.syntheticsWorkers {
		infos = append(infos, syntheticsWorkerInfo{
			WorkerID:      w.workerID,
			LocationID:    w.locationID,
			WorkerVersion: w.workerVersion,
			Authorization: w.authorization,
			ConnectedAt:   w.connectedAt,
			Heartbeats:    w.heartbeats,
			LastHeartbeat: w.lastHeartbeat,
		})
	}
	return infos
}

// lookupSyntheticsWorkerByLocation returns the most recently connected synthetics-worker for the given location ID.
func (s *state) lookupSyntheticsWorkerByLocation(locationID string) *syntheticsWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *syntheticsWorker
	for _, w := range s.syntheticsWorkers {
		if w.locationID == locationID && (latest == nil || w.connectedAt.After(latest.connectedAt)) {
			latest = w
		}
	}
	return latest
}

func (s *state) recordSyntheticsTaskResult(result syntheticsTaskResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syntheticsTaskResults = append(s.syntheticsTaskResults, result)
}

func (s *state) listSyntheticsTaskResults() []syntheticsTaskResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make([]syntheticsTaskResult, len(s.syntheticsTaskResults))
	copy(results, s.syntheticsTaskResults)
	return results
}

type syntheticTaskServer struct {
	pb.UnimplementedSyntheticTaskServiceServer
	state *state
}

// Subscribe handles the bidirectional stream opened by a synthetics-worker. It requires a Hello as the first message,
// records the worker (and the gRPC metadata it announced itself with) so the e2e test can assert the connection was
// established correctly, counts heartbeats, forwards TaskRequests queued via the HTTP debug API down the stream, and
// stores every TaskResult received back from the worker.
func (s *syntheticTaskServer) Subscribe(stream grpc.BidiStreamingServer[pb.WorkerMessage, pb.ServerMessage]) error {
	ctx := stream.Context()

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "the first message on the stream must be a Hello")
	}

	_, authorization := metadataFromContext(ctx)
	worker := &syntheticsWorker{
		workerID:      hello.GetWorkerId(),
		locationID:    hello.GetLocationId(),
		workerVersion: hello.GetWorkerVersion(),
		authorization: authorization,
		connectedAt:   time.Now().UTC(),
		sendChan:      make(chan *pb.TaskRequest, 16),
	}
	s.state.registerSyntheticsWorker(worker)
	defer s.state.unregisterSyntheticsWorker(worker)

	log.Printf(
		"synthetics-worker subscribed (worker_id=%q, location_id=%q, worker_version=%q, authorization=%q)",
		worker.workerID,
		worker.locationID,
		worker.workerVersion,
		authorization,
	)
	defer log.Printf(
		"synthetics-worker unsubscribed (worker_id=%q, location_id=%q)", worker.workerID, worker.locationID)

	// Reader goroutine: receive heartbeats and task results from the worker.
	errChan := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					errChan <- nil
					return
				}
				errChan <- err
				return
			}
			switch {
			case msg.GetHeartbeat() != nil:
				s.state.recordSyntheticsHeartbeat(worker)
			case msg.GetTaskResult() != nil:
				s.recordTaskResult(worker, msg.GetTaskResult())
			}
		}
	}()

	// Writer loop: forward task requests queued via the HTTP debug API down the stream.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case req := <-worker.sendChan:
			if err := stream.Send(&pb.ServerMessage{
				Message: &pb.ServerMessage_TaskRequest{TaskRequest: req},
			}); err != nil {
				return err
			}
			log.Printf("sent task request (worker_id=%q, task_id=%q)", worker.workerID, req.GetTaskId())
		}
	}
}

func (s *syntheticTaskServer) recordTaskResult(worker *syntheticsWorker, taskResult *pb.TaskResult) {
	result := syntheticsTaskResult{
		TaskID:   taskResult.GetTaskId(),
		WorkerID: worker.workerID,
	}
	if httpCheck := taskResult.GetHttpCheck(); httpCheck != nil {
		result.ApiVersion = httpCheck.GetApiVersion()
		result.Result = httpCheck.GetResult()
	}
	if failure := taskResult.GetFailure(); failure != nil {
		result.FailureReason = failure.GetReason().String()
		result.FailureMessage = failure.GetMessage()
	}
	s.state.recordSyntheticsTaskResult(result)
	log.Printf(
		"received task result (worker_id=%q, task_id=%q, failure_reason=%q)",
		worker.workerID,
		result.TaskID,
		result.FailureReason,
	)
}

// handleTriggerSyntheticsTaskRequest pushes an HTTP check TaskRequest for the given URL down the stream of the most
// recently connected synthetics-worker for the given location. The check carries a single critical assertion,
// expecting the status code 204. The mock generates the task ID and returns it so the caller can correlate it with the
// eventual TaskResult.
func handleTriggerSyntheticsTaskRequest(st *state, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body triggerSyntheticsTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.LocationID == "" {
		http.Error(w, "locationId is required", http.StatusBadRequest)
		return
	}
	if body.Url == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	worker := st.lookupSyntheticsWorkerByLocation(body.LocationID)
	if worker == nil {
		http.Error(w, "no synthetics-worker connected for locationId "+body.LocationID, http.StatusNotFound)
		return
	}

	req, err := newHttpCheckTaskRequest(body.Url)
	if err != nil {
		http.Error(w, "cannot assemble the task request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	select {
	case worker.sendChan <- req:
		writeJSON(w, triggerSyntheticsTaskResponse{TaskID: req.GetTaskId(), WorkerID: worker.workerID})
	case <-time.After(sendTimeout):
		http.Error(
			w,
			"timed out queueing task request for synthetics-worker "+worker.workerID,
			http.StatusServiceUnavailable,
		)
	}
}

// newHttpCheckTaskRequest assembles a TaskRequest for an HTTP check that sends a GET request to the given URL. The
// params are the JSON encoding of the Dash0 OpenAPI schema syntheticchecks.HttpRequestParams. The synthetics-worker
// derives the trace ID of the check from checkId and attemptId, which therefore need to be a UUID and a hex string,
// respectively.
func newHttpCheckTaskRequest(url string) (*pb.TaskRequest, error) {
	attemptID, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	params, err := json.Marshal(map[string]any{
		"organizationTechnicalId": "e2e-test-organization",
		"checkId":                 uuid.NewString(),
		"attemptId":               attemptID,
		"request": map[string]any{
			"method":    "get",
			"url":       url,
			"redirects": "follow",
			"tls":       map[string]any{"allowInsecure": false},
			"tracing":   map[string]any{"addTracingHeaders": false},
		},
		"assertions": map[string]any{
			"criticalAssertions": []any{
				map[string]any{
					"kind": "status_code",
					"spec": map[string]any{"operator": "is", "value": "204"},
				},
			},
			"degradedAssertions": []any{},
		},
		"timeout": httpCheckTimeout,
	})
	if err != nil {
		return nil, err
	}

	traceID, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	spanID, err := randomHex(8)
	if err != nil {
		return nil, err
	}

	return &pb.TaskRequest{
		TaskId:      uuid.NewString(),
		Traceparent: fmt.Sprintf("00-%s-%s-01", traceID, spanID),
		Timeout:     durationpb.New(httpCheckTaskTimeout),
		Payload: &pb.TaskRequest_HttpCheck{
			HttpCheck: &pb.HttpCheckTask{
				Params:     params,
				ApiVersion: httpCheckApiVersion,
			},
		},
	}, nil
}

func randomHex(numberOfBytes int) (string, error) {
	b := make([]byte, numberOfBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
