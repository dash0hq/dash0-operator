// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/parca-dev/parca/gen/proto/go/parca/debuginfo/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type backend struct {
	pb.UnimplementedDebuginfoServiceServer
	check    func(context.Context, *pb.ShouldInitiateUploadRequest) (*pb.ShouldInitiateUploadResponse, error)
	initiate func(context.Context, *pb.InitiateUploadRequest) (*pb.InitiateUploadResponse, error)
	finish   func(context.Context, *pb.MarkUploadFinishedRequest) (*pb.MarkUploadFinishedResponse, error)
	upload   func(pb.DebuginfoService_UploadServer) error
}

func (b *backend) ShouldInitiateUpload(
	ctx context.Context, req *pb.ShouldInitiateUploadRequest,
) (*pb.ShouldInitiateUploadResponse, error) {
	if b.check != nil {
		return b.check(ctx, req)
	}
	return &pb.ShouldInitiateUploadResponse{ShouldInitiateUpload: true}, nil
}

func (b *backend) InitiateUpload(
	ctx context.Context, req *pb.InitiateUploadRequest,
) (*pb.InitiateUploadResponse, error) {
	if b.initiate != nil {
		return b.initiate(ctx, req)
	}
	return &pb.InitiateUploadResponse{
		UploadTargets: []*pb.UploadInstructions{instruction(req, "opaque", "https://s3.test/x")},
	}, nil
}

func (b *backend) MarkUploadFinished(
	ctx context.Context, req *pb.MarkUploadFinishedRequest,
) (*pb.MarkUploadFinishedResponse, error) {
	if b.finish != nil {
		return b.finish(ctx, req)
	}
	return &pb.MarkUploadFinishedResponse{}, nil
}

func (b *backend) Upload(stream pb.DebuginfoService_UploadServer) error {
	return b.upload(stream)
}

func serve(t *testing.T, server pb.DebuginfoServiceServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := grpc.NewServer()
	pb.RegisterDebuginfoServiceServer(s, server)
	t.Cleanup(s.Stop)
	go func() { _ = s.Serve(listener) }()
	return listener.Addr().String()
}

func client(t *testing.T, endpoint string) pb.DebuginfoServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewDebuginfoServiceClient(conn)
}

func testConfig(t *testing.T, endpoints ...string) *config {
	t.Helper()
	t.Setenv("PARCA_PRIMARY_TOKEN", "test-primary")
	t.Setenv("PARCA_SECONDARY_TOKEN", "test-secondary")
	c, err := loadConfig(t.Context(), "../config.example.yaml")
	require.NoError(t, err)
	r := c.Receivers["debuginfo"]
	r.GRPC.TLS = nil
	c.Receivers["debuginfo"] = r
	c.Exporters = make(map[string]exporterConfig)
	ids := make([]string, 0, len(endpoints))
	for i, endpoint := range endpoints {
		id := fmt.Sprintf("debuginfo/backend-%d", i)
		e := exporterConfig{Endpoint: endpoint, Headers: map[string]string{"authorization": "Bearer backend-credential"}}
		e.TLS.Insecure = true
		c.Exporters[id] = e
		ids = append(ids, id)
	}
	c.Service.Pipelines["debuginfo"] = pipelineConfig{Receivers: []string{"debuginfo"}, Exporters: ids}
	return c
}

func startProxy(t *testing.T, c *config) (*proxy, pb.DebuginfoServiceClient) {
	t.Helper()
	p, closeConnections, err := newProxy(t.Context(), c)
	require.NoError(t, err)
	t.Cleanup(closeConnections)
	return p, client(t, serve(t, p))
}

func request() *pb.InitiateUploadRequest {
	return &pb.InitiateUploadRequest{
		BuildId: "abc123", Hash: "sha256:123", Size: 731,
		Type: pb.DebuginfoType_DEBUGINFO_TYPE_EXECUTABLE, BuildIdType: pb.BuildIDType_BUILD_ID_TYPE_GNU,
		SupportsMultipleUploads: true,
	}
}

func instruction(req *pb.InitiateUploadRequest, id, signedURL string) *pb.UploadInstructions {
	return &pb.UploadInstructions{
		BuildId: req.BuildId, Type: req.Type, UploadId: id, SignedUrl: signedURL,
		UploadStrategy: pb.UploadInstructions_UPLOAD_STRATEGY_SIGNED_URL,
	}
}

func completion(target *pb.UploadInstructions) *pb.MarkUploadFinishedRequest {
	return &pb.MarkUploadFinishedRequest{BuildId: target.BuildId, Type: target.Type, UploadId: target.UploadId}
}

func TestSingleUpstreamPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name  string
		multi bool
		force bool
	}{
		{name: "legacy"},
		{name: "forced", force: true},
		{name: "multiple targets", multi: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request()
			req.SupportsMultipleUploads = tc.multi
			req.Force = tc.force
			checkReq := &pb.ShouldInitiateUploadRequest{BuildId: req.BuildId, Force: tc.force}
			checkResp := &pb.ShouldInitiateUploadResponse{Reason: reasonUploadInProgress}
			instructions := instruction(req, "upstream/opaque+/=", "https://s3.test/object?sig=a%2Fb")
			instructions.UploadStrategy = pb.UploadInstructions_UPLOAD_STRATEGY_GRPC
			plan := &pb.InitiateUploadResponse{UploadInstructions: instructions}
			if tc.multi {
				plan = &pb.InitiateUploadResponse{UploadTargets: []*pb.UploadInstructions{instructions}, RetryRequired: true}
			}
			b := &backend{
				check: func(_ context.Context, got *pb.ShouldInitiateUploadRequest) (*pb.ShouldInitiateUploadResponse, error) {
					if !proto.Equal(checkReq, got) {
						return nil, status.Error(codes.InvalidArgument, "preflight request changed")
					}
					return checkResp, nil
				},
				initiate: func(ctx context.Context, got *pb.InitiateUploadRequest) (*pb.InitiateUploadResponse, error) {
					md, _ := metadata.FromIncomingContext(ctx)
					if !proto.Equal(req, got) ||
						!slices.Equal(md.Get("authorization"), []string{"Bearer backend-credential"}) {
						return nil, status.Error(codes.InvalidArgument, "initiation request or credentials changed")
					}
					return plan, nil
				},
				finish: func(_ context.Context, got *pb.MarkUploadFinishedRequest) (*pb.MarkUploadFinishedResponse, error) {
					if !proto.Equal(completion(instructions), got) {
						return nil, status.Error(codes.InvalidArgument, "completion request changed")
					}
					return nil, status.Error(codes.AlreadyExists, "upstream completion diagnostic")
				},
			}
			_, cl := startProxy(t, testConfig(t, serve(t, b)))
			check, err := cl.ShouldInitiateUpload(t.Context(), checkReq)
			require.NoError(t, err)
			require.True(t, proto.Equal(checkResp, check))
			got, err := cl.InitiateUpload(t.Context(), req)
			require.NoError(t, err)
			require.True(t, proto.Equal(plan, got))
			_, err = cl.MarkUploadFinished(t.Context(), completion(instructions))
			require.Equal(t, codes.AlreadyExists, status.Code(err))
			require.Equal(t, "upstream completion diagnostic", status.Convert(err).Message())
		})
	}
}

func TestSingleUpstreamStreamingUpload(t *testing.T) {
	info := &pb.UploadRequest{Data: &pb.UploadRequest_Info{Info: &pb.UploadInfo{
		BuildId: "stream-build", UploadId: "upstream-opaque", Type: pb.DebuginfoType_DEBUGINFO_TYPE_EXECUTABLE,
	}}}
	chunk := &pb.UploadRequest{Data: &pb.UploadRequest_ChunkData{ChunkData: []byte("artifact bytes")}}
	b := &backend{upload: func(stream pb.DebuginfoService_UploadServer) error {
		md, _ := metadata.FromIncomingContext(stream.Context())
		if !slices.Equal(md.Get("authorization"), []string{"Bearer backend-credential"}) ||
			len(md.Get("x-agent-metadata")) != 0 {
			return status.Error(codes.Unauthenticated, "incorrect exporter credentials")
		}
		for _, want := range []*pb.UploadRequest{info, chunk, chunk} {
			got, err := stream.Recv()
			if err != nil {
				return err
			}
			if !proto.Equal(want, got) {
				return status.Error(codes.InvalidArgument, "stream request changed")
			}
		}
		if _, err := stream.Recv(); err != io.EOF {
			return status.Error(codes.InvalidArgument, "expected stream end")
		}
		return stream.SendAndClose(&pb.UploadResponse{BuildId: "stream-build", Size: 28})
	}}
	_, cl := startProxy(t, testConfig(t, serve(t, b)))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-agent-metadata", "do not forward"))
	stream, err := cl.Upload(ctx)
	require.NoError(t, err)
	for _, message := range []*pb.UploadRequest{info, chunk, chunk} {
		require.NoError(t, stream.Send(message))
	}
	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	require.Equal(t, "stream-build", resp.BuildId)
	require.EqualValues(t, 28, resp.Size)

	stream, err = cl.Upload(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(chunk))
	_, err = stream.CloseAndRecv()
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "stream request changed", status.Convert(err).Message())
}

func TestSingleUpstreamStreamingCancellation(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	b := &backend{upload: func(stream pb.DebuginfoService_UploadServer) error {
		close(started)
		<-stream.Context().Done()
		close(canceled)
		return status.FromContextError(stream.Context().Err()).Err()
	}}
	_, cl := startProxy(t, testConfig(t, serve(t, b)))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := cl.Upload(ctx)
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream stream did not start")
	}
	cancel()
	_, err = stream.CloseAndRecv()
	require.Equal(t, codes.Canceled, status.Code(err))
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream stream was not canceled")
	}
}

func TestSignedUploadsAndReplicaCompletion(t *testing.T) {
	var mu sync.Mutex
	stored := map[string][]byte{}
	finished := map[string][]string{}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.RawQuery != "sig=a%2Fb&part=2&part=1" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected upload request", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read upload", http.StatusBadRequest)
			return
		}
		mu.Lock()
		stored[r.URL.Path] = data
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(storage.Close)
	endpoints := make([]string, 0, 2)
	for _, name := range []string{"first", "second"} {
		b := &backend{
			initiate: func(ctx context.Context, req *pb.InitiateUploadRequest) (*pb.InitiateUploadResponse, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				if !req.SupportsMultipleUploads || req.Size != 731 || req.Hash != "sha256:123" ||
					req.BuildIdType != pb.BuildIDType_BUILD_ID_TYPE_GNU ||
					!slices.Equal(md.Get("authorization"), []string{"Bearer backend-credential"}) ||
					len(md.Get("x-agent-metadata")) != 0 {
					return nil, status.Error(codes.InvalidArgument, "incorrect forwarded negotiation or credentials")
				}
				target := instruction(req, name+"/opaque+/=", storage.URL+"/"+name+"?sig=a%2Fb&part=2&part=1")
				if name == "first" {
					return &pb.InitiateUploadResponse{UploadInstructions: target}, nil
				}
				return &pb.InitiateUploadResponse{UploadTargets: []*pb.UploadInstructions{target}}, nil
			},
			finish: func(ctx context.Context, req *pb.MarkUploadFinishedRequest) (*pb.MarkUploadFinishedResponse, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				if req.UploadId != name+"/opaque+/=" || req.BuildId != "abc123" ||
					req.Type != pb.DebuginfoType_DEBUGINFO_TYPE_EXECUTABLE ||
					!slices.Equal(md.Get("authorization"), []string{"Bearer backend-credential"}) ||
					len(md.Get("x-agent-metadata")) != 0 {
					return nil, status.Error(codes.InvalidArgument, "completion reached wrong upload")
				}
				mu.Lock()
				defer mu.Unlock()
				finished[name] = append(finished[name], req.UploadId)
				return &pb.MarkUploadFinishedResponse{}, nil
			},
		}
		endpoints = append(endpoints, serve(t, b))
	}
	c := testConfig(t, endpoints...)
	_, first := startProxy(t, c)
	check, err := first.ShouldInitiateUpload(t.Context(), &pb.ShouldInitiateUploadRequest{BuildId: request().BuildId})
	require.NoError(t, err)
	require.True(t, check.ShouldInitiateUpload)
	plan, err := first.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	require.Nil(t, plan.UploadInstructions)
	require.False(t, plan.RetryRequired)
	require.Len(t, plan.UploadTargets, 2)
	pipeline := c.Service.Pipelines["debuginfo"]
	slices.Reverse(pipeline.Exporters)
	c.Service.Pipelines["debuginfo"] = pipeline
	_, replica := startProxy(t, c)
	artifact := []byte("independently uploaded executable bytes")
	for i, name := range []string{"first", "second"} {
		target := plan.UploadTargets[i]
		require.Equal(t, storage.URL+"/"+name+"?sig=a%2Fb&part=2&part=1", target.SignedUrl)
		require.NotEqual(t, name+"/opaque+/=", target.UploadId)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, target.SignedUrl, bytes.NewReader(artifact))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_, err = replica.MarkUploadFinished(t.Context(), completion(target))
		require.NoError(t, err)
	}
	callerMetadata := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		"authorization", "Bearer agent-credential", "x-agent-metadata", "not-for-the-backend"))
	_, err = replica.MarkUploadFinished(callerMetadata, completion(plan.UploadTargets[0]))
	require.NoError(t, err)
	_, err = first.InitiateUpload(callerMetadata, request())
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, artifact, stored["/first"])
	require.Equal(t, artifact, stored["/second"])
	require.Equal(t, []string{"first/opaque+/=", "first/opaque+/="}, finished["first"])
	require.Equal(t, []string{"second/opaque+/="}, finished["second"])
}

func TestMixedNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		yes         bool
		firstHint   bool
		temporary   bool
		fail        codes.Code
		wantCode    codes.Code
		wantYes     bool
		wantRetry   bool
		wantTargets int
	}{
		{name: "success despite outage", yes: true, fail: codes.Unavailable, wantYes: true, wantRetry: true, wantTargets: 1},
		{name: "no definitive no on outage", fail: codes.Unavailable, wantCode: codes.Unavailable},
		{name: "all present"},
		{name: "legacy in progress", temporary: true, wantRetry: true},
		{name: "success and pending", yes: true, temporary: true, wantYes: true, wantRetry: true, wantTargets: 1},
		{name: "affirmative preflight hint", yes: true, firstHint: true, wantYes: true, wantRetry: true, wantTargets: 1},
		{name: "permission needs action", fail: codes.PermissionDenied, wantCode: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b1 := &backend{check: func(
				context.Context, *pb.ShouldInitiateUploadRequest,
			) (*pb.ShouldInitiateUploadResponse, error) {
				return &pb.ShouldInitiateUploadResponse{ShouldInitiateUpload: tc.yes, RetryRequired: tc.firstHint}, nil
			}}
			b2 := &backend{check: func(
				context.Context, *pb.ShouldInitiateUploadRequest,
			) (*pb.ShouldInitiateUploadResponse, error) {
				if tc.fail != codes.OK {
					return nil, status.Error(tc.fail, "backend failure")
				}
				resp := &pb.ShouldInitiateUploadResponse{}
				if tc.temporary {
					resp.Reason = reasonUploadInProgress
				}
				return resp, nil
			}}
			_, cl := startProxy(t, testConfig(t, serve(t, b1), serve(t, b2)))
			check, err := cl.ShouldInitiateUpload(t.Context(), &pb.ShouldInitiateUploadRequest{BuildId: "abc123"})
			require.Equal(t, tc.wantCode, status.Code(err))
			if err == nil {
				require.Equal(t, tc.wantYes, check.ShouldInitiateUpload)
				require.Equal(t, tc.wantRetry, check.RetryRequired)
			}
			plan, err := cl.InitiateUpload(t.Context(), request())
			require.Equal(t, tc.wantCode, status.Code(err))
			if err == nil {
				require.Len(t, plan.UploadTargets, tc.wantTargets)
				require.Equal(t, tc.wantRetry, plan.RetryRequired)
			}
		})
	}
}

func TestInitiationRacesAndUnsupportedTargets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      codes.Code
		recheck   *pb.ShouldInitiateUploadResponse
		strategy  pb.UploadInstructions_UploadStrategy
		wantCode  codes.Code
		wantRetry bool
	}{
		{name: "concurrent upload", code: codes.FailedPrecondition,
			recheck: &pb.ShouldInitiateUploadResponse{RetryRequired: true}, wantRetry: true},
		{name: "concurrent completion", code: codes.FailedPrecondition, recheck: &pb.ShouldInitiateUploadResponse{}},
		{name: "configuration refusal", code: codes.FailedPrecondition,
			recheck: &pb.ShouldInitiateUploadResponse{ShouldInitiateUpload: true}, wantCode: codes.FailedPrecondition},
		{name: "invalid artifact is not present", code: codes.AlreadyExists, wantCode: codes.AlreadyExists},
		{name: "grpc bytes unsupported", strategy: pb.UploadInstructions_UPLOAD_STRATEGY_GRPC,
			wantCode: codes.FailedPrecondition},
		{name: "unknown strategy", strategy: pb.UploadInstructions_UPLOAD_STRATEGY_UNSPECIFIED,
			wantCode: codes.FailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var initiated atomic.Bool
			b := &backend{
				check: func(context.Context, *pb.ShouldInitiateUploadRequest) (*pb.ShouldInitiateUploadResponse, error) {
					if initiated.Load() && tc.recheck != nil {
						return tc.recheck, nil
					}
					return &pb.ShouldInitiateUploadResponse{ShouldInitiateUpload: true}, nil
				},
				initiate: func(_ context.Context, req *pb.InitiateUploadRequest) (*pb.InitiateUploadResponse, error) {
					initiated.Store(true)
					if tc.code != codes.OK {
						return nil, status.Error(tc.code, "diagnostic refusal")
					}
					target := instruction(req, "opaque", "https://s3.test/object")
					target.UploadStrategy = tc.strategy
					return &pb.InitiateUploadResponse{UploadInstructions: target}, nil
				},
			}
			_, cl := startProxy(t, testConfig(t, serve(t, b), serve(t, b)))
			plan, err := cl.InitiateUpload(t.Context(), request())
			require.Equal(t, tc.wantCode, status.Code(err))
			if tc.code == codes.AlreadyExists {
				require.Contains(t, err.Error(), "diagnostic refusal")
			}
			if err == nil {
				require.Empty(t, plan.UploadTargets)
				require.Equal(t, tc.wantRetry, plan.RetryRequired)
			}
		})
	}
}

func TestCompletionRouting(t *testing.T) {
	var completions atomic.Int32
	b := &backend{finish: func(context.Context, *pb.MarkUploadFinishedRequest) (*pb.MarkUploadFinishedResponse, error) {
		completions.Add(1)
		return &pb.MarkUploadFinishedResponse{}, nil
	}}
	c := testConfig(t, serve(t, b), serve(t, b))
	p, cl := startProxy(t, c)
	plan, err := cl.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	valid := completion(plan.UploadTargets[0])
	for _, tc := range []struct {
		name   string
		mutate func(*pb.MarkUploadFinishedRequest)
	}{
		{name: "build", mutate: func(r *pb.MarkUploadFinishedRequest) { r.BuildId = "another-build" }},
		{name: "type", mutate: func(r *pb.MarkUploadFinishedRequest) { r.Type = pb.DebuginfoType_DEBUGINFO_TYPE_SOURCES }},
		{name: "invalid JSON", mutate: func(r *pb.MarkUploadFinishedRequest) {
			r.UploadId = base64.RawURLEncoding.EncodeToString([]byte("not JSON"))
		}},
		{name: "malformed", mutate: func(r *pb.MarkUploadFinishedRequest) { r.UploadId = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := proto.Clone(valid).(*pb.MarkUploadFinishedRequest)
			tc.mutate(req)
			_, err := cl.MarkUploadFinished(t.Context(), req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
	p.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	_, err = cl.MarkUploadFinished(t.Context(), valid)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, replica := startProxy(t, c)
	e := c.Exporters["debuginfo/backend-0"]
	e.Headers["authorization"] = "Bearer different-backend-tenant"
	c.Exporters["debuginfo/backend-0"] = e
	_, replaced := startProxy(t, c)
	_, err = replaced.MarkUploadFinished(t.Context(), valid)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = replica.MarkUploadFinished(t.Context(), valid)
	require.NoError(t, err)
	require.EqualValues(t, 1, completions.Load())
}

func TestNestedCollectors(t *testing.T) {
	var completed atomic.Int32
	b := &backend{finish: func(
		_ context.Context, req *pb.MarkUploadFinishedRequest,
	) (*pb.MarkUploadFinishedResponse, error) {
		if req.UploadId != "opaque" {
			return nil, status.Error(codes.InvalidArgument, "opaque ID changed")
		}
		completed.Add(1)
		return &pb.MarkUploadFinishedResponse{}, nil
	}}
	c := testConfig(t, serve(t, b), serve(t, b))
	inner, _ := startProxy(t, c)
	outerConfig := testConfig(t, serve(t, inner))
	_, outer := startProxy(t, outerConfig)
	plan, err := outer.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	require.Len(t, plan.UploadTargets, 2)
	for _, target := range plan.UploadTargets {
		_, err = outer.MarkUploadFinished(t.Context(), completion(target))
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, completed.Load())
}

func TestPartialInitiationAndIndependentCompletion(t *testing.T) {
	var attempts, successes atomic.Int32
	flaky := &backend{finish: func(
		context.Context, *pb.MarkUploadFinishedRequest,
	) (*pb.MarkUploadFinishedResponse, error) {
		if attempts.Add(1) == 1 {
			return nil, status.Error(codes.Unavailable, "temporary completion failure")
		}
		return &pb.MarkUploadFinishedResponse{}, nil
	}}
	healthy := &backend{finish: func(
		context.Context, *pb.MarkUploadFinishedRequest,
	) (*pb.MarkUploadFinishedResponse, error) {
		successes.Add(1)
		return &pb.MarkUploadFinishedResponse{}, nil
	}}
	refused := &backend{initiate: func(
		context.Context, *pb.InitiateUploadRequest,
	) (*pb.InitiateUploadResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "destination requires operator action")
	}}
	_, cl := startProxy(t, testConfig(t, serve(t, flaky), serve(t, healthy), serve(t, refused)))
	plan, err := cl.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	require.True(t, plan.RetryRequired)
	require.Len(t, plan.UploadTargets, 2)
	_, err = cl.MarkUploadFinished(t.Context(), completion(plan.UploadTargets[0]))
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, err = cl.MarkUploadFinished(t.Context(), completion(plan.UploadTargets[1]))
	require.NoError(t, err)
	require.EqualValues(t, 1, successes.Load())
	_, err = cl.MarkUploadFinished(t.Context(), completion(plan.UploadTargets[0]))
	require.NoError(t, err)
	require.EqualValues(t, 2, attempts.Load())
}

func TestLostInitiationAndStaleCompletion(t *testing.T) {
	var mu sync.Mutex
	var generation int
	var pending bool
	var finishedID string
	b := &backend{
		check: func(context.Context, *pb.ShouldInitiateUploadRequest) (*pb.ShouldInitiateUploadResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			return &pb.ShouldInitiateUploadResponse{ShouldInitiateUpload: !pending, RetryRequired: pending}, nil
		},
		initiate: func(_ context.Context, req *pb.InitiateUploadRequest) (*pb.InitiateUploadResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			generation++
			pending = true
			return &pb.InitiateUploadResponse{UploadTargets: []*pb.UploadInstructions{
				instruction(req, fmt.Sprint(generation), "https://s3.test/artifact"),
			}}, nil
		},
		finish: func(_ context.Context, req *pb.MarkUploadFinishedRequest) (*pb.MarkUploadFinishedResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			if req.UploadId != fmt.Sprint(generation) {
				return nil, status.Error(codes.InvalidArgument, "stale downstream upload ID")
			}
			finishedID = req.UploadId
			return &pb.MarkUploadFinishedResponse{}, nil
		},
	}
	present := &backend{check: func(
		context.Context, *pb.ShouldInitiateUploadRequest,
	) (*pb.ShouldInitiateUploadResponse, error) {
		return &pb.ShouldInitiateUploadResponse{}, nil
	}}
	_, cl := startProxy(t, testConfig(t, serve(t, b), serve(t, present)))
	lost, err := cl.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	check, err := cl.ShouldInitiateUpload(t.Context(), &pb.ShouldInitiateUploadRequest{BuildId: request().BuildId})
	require.NoError(t, err)
	require.False(t, check.ShouldInitiateUpload)
	require.True(t, check.RetryRequired)
	again, err := cl.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	require.Empty(t, again.UploadTargets)
	require.True(t, again.RetryRequired)
	mu.Lock()
	pending = false
	mu.Unlock()
	current, err := cl.InitiateUpload(t.Context(), request())
	require.NoError(t, err)
	_, err = cl.MarkUploadFinished(t.Context(), completion(lost.UploadTargets[0]))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = cl.MarkUploadFinished(t.Context(), completion(current.UploadTargets[0]))
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "2", finishedID)
}

func TestGatingAndDeadline(t *testing.T) {
	var contacted atomic.Int32
	b := &backend{check: func(
		ctx context.Context, _ *pb.ShouldInitiateUploadRequest,
	) (*pb.ShouldInitiateUploadResponse, error) {
		contacted.Add(1)
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}}
	c := testConfig(t, serve(t, b), serve(t, b))
	_, cl := startProxy(t, c)
	legacy := request()
	legacy.SupportsMultipleUploads = false
	_, err := cl.InitiateUpload(t.Context(), legacy)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	forced := request()
	forced.Force = true
	_, err = cl.InitiateUpload(t.Context(), forced)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Zero(t, contacted.Load())
	stream, err := cl.Upload(t.Context())
	require.NoError(t, err)
	_, err = stream.CloseAndRecv()
	require.Equal(t, codes.Unimplemented, status.Code(err))
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err = cl.InitiateUpload(ctx, request())
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestConfigurationValidation(t *testing.T) {
	c := testConfig(t, "127.0.0.1:7071", "127.0.0.1:7072")
	require.NoError(t, c.validate())
	delete(c.Exporters, "debuginfo/backend-1")
	require.Error(t, c.validate())
	data, err := os.ReadFile("../config.example.yaml")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	invalid := strings.Replace(string(data), "endpoint:", "endpoint_typo:", 1)
	require.NoError(t, os.WriteFile(path, []byte(invalid), 0600))
	_, err = loadConfig(t.Context(), path)
	require.Error(t, err)
}
