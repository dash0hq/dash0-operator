// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	pb "github.com/parca-dev/parca/gen/proto/go/parca/debuginfo/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const reasonUploadInProgress = "A previous upload is still in-progress and not stale yet " +
	"(only stale uploads can be retried)."

const (
	defaultTimeout        = 10 * time.Second
	defaultRouteTTL       = 15 * time.Minute
	defaultMaxConcurrency = 8
)

type destination struct {
	id       string
	identity string
	client   pb.DebuginfoServiceClient
	headers  metadata.MD
}

type proxy struct {
	pb.UnimplementedDebuginfoServiceServer
	destinations []destination
	slots        chan struct{}
	now          func() time.Time
}

type route struct {
	Destination string           `json:"destination"`
	Identity    string           `json:"identity"`
	UploadID    string           `json:"upload_id"`
	BuildID     string           `json:"build_id"`
	Type        pb.DebuginfoType `json:"type"`
	Expires     int64            `json:"expires"`
}

func (p *proxy) downstream(ctx context.Context, d destination) context.Context {
	return metadata.NewOutgoingContext(ctx, d.headers)
}

func (p *proxy) fanout(ctx context.Context, call func(context.Context, destination, int) error) []error {
	var wg sync.WaitGroup
	errs := make([]error, len(p.destinations))
	for i, d := range p.destinations {
		wg.Go(func() {
			select {
			case p.slots <- struct{}{}:
				defer func() { <-p.slots }()
			case <-ctx.Done():
				errs[i] = status.FromContextError(ctx.Err()).Err()
				return
			}
			errs[i] = call(p.downstream(ctx, d), d, i)
		})
	}
	wg.Wait()
	return errs
}

// ShouldInitiateUpload asks every destination, retaining temporary refusals as retry hints.
func (p *proxy) ShouldInitiateUpload(
	ctx context.Context, req *pb.ShouldInitiateUploadRequest,
) (*pb.ShouldInitiateUploadResponse, error) {
	if len(p.destinations) == 1 {
		d := p.destinations[0]
		return d.client.ShouldInitiateUpload(p.downstream(ctx, d), req)
	}
	if req.Force {
		return nil, status.Error(codes.FailedPrecondition, "forced fanout is not supported")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	checks := make([]*pb.ShouldInitiateUploadResponse, len(p.destinations))
	errs := p.fanout(ctx, func(ctx context.Context, d destination, i int) error {
		var err error
		checks[i], err = d.client.ShouldInitiateUpload(ctx, req)
		return err
	})
	resp := &pb.ShouldInitiateUploadResponse{}
	reasons := make([]string, 0, len(checks))
	for i, check := range checks {
		if errs[i] != nil {
			resp.RetryRequired = true
			continue
		}
		resp.ShouldInitiateUpload = resp.ShouldInitiateUpload || check.GetShouldInitiateUpload()
		resp.RetryRequired = resp.RetryRequired || retryCheck(check)
		reasons = append(reasons, p.destinations[i].id+": "+check.GetReason())
	}
	if !resp.ShouldInitiateUpload {
		if err := p.failure(errs); err != nil {
			return nil, err
		}
	}
	resp.Reason = strings.Join(reasons, "; ")
	return resp, nil
}

func retryCheck(check *pb.ShouldInitiateUploadResponse) bool {
	return check.GetRetryRequired() || check.GetReason() == reasonUploadInProgress
}

// InitiateUpload negotiates independent signed-URL uploads without transferring artifact bytes.
func (p *proxy) InitiateUpload(
	ctx context.Context, req *pb.InitiateUploadRequest,
) (*pb.InitiateUploadResponse, error) {
	if len(p.destinations) == 1 {
		d := p.destinations[0]
		return d.client.InitiateUpload(p.downstream(ctx, d), req)
	}
	if !req.SupportsMultipleUploads || req.Force {
		return nil, status.Error(codes.FailedPrecondition, "multi-upload capability required; forced fanout unsupported")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	plans := make([]*pb.InitiateUploadResponse, len(p.destinations))
	errs := p.fanout(ctx, func(ctx context.Context, d destination, i int) error {
		var err error
		plans[i], err = p.negotiate(ctx, d, req)
		return err
	})
	resp := &pb.InitiateUploadResponse{}
	for i, plan := range plans {
		if errs[i] != nil {
			resp.RetryRequired = true
			continue
		}
		resp.RetryRequired = resp.RetryRequired || plan.GetRetryRequired()
		targets := plan.GetUploadTargets()
		if plan.GetUploadInstructions() != nil {
			if len(targets) != 0 {
				errs[i] = status.Error(codes.FailedPrecondition, "backend returned both instruction shapes")
				resp.RetryRequired = true
				continue
			}
			targets = []*pb.UploadInstructions{plan.UploadInstructions}
		}
		for _, target := range targets {
			wrapped, err := p.wrap(req, p.destinations[i], target)
			if err != nil {
				errs[i] = err
				resp.RetryRequired = true
				continue
			}
			resp.UploadTargets = append(resp.UploadTargets, wrapped)
		}
	}
	if len(resp.UploadTargets) == 0 {
		if err := p.failure(errs); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func (p *proxy) negotiate(
	ctx context.Context, d destination, req *pb.InitiateUploadRequest,
) (*pb.InitiateUploadResponse, error) {
	checkReq := &pb.ShouldInitiateUploadRequest{
		BuildId: req.BuildId, Hash: req.Hash, Type: req.Type, BuildIdType: req.BuildIdType,
	}
	check, err := d.client.ShouldInitiateUpload(ctx, checkReq)
	if err != nil {
		return nil, err
	}
	if !check.GetShouldInitiateUpload() {
		return &pb.InitiateUploadResponse{RetryRequired: retryCheck(check)}, nil
	}
	plan, err := d.client.InitiateUpload(ctx, req)
	if err == nil {
		plan.RetryRequired = plan.RetryRequired || retryCheck(check)
		return plan, nil
	}
	if status.Code(err) != codes.FailedPrecondition {
		return plan, err
	}
	recheck, checkErr := d.client.ShouldInitiateUpload(ctx, checkReq)
	if checkErr != nil || recheck.GetShouldInitiateUpload() {
		return nil, err
	}
	return &pb.InitiateUploadResponse{RetryRequired: retryCheck(recheck)}, nil
}

func (p *proxy) failure(errs []error) error {
	for i, err := range errs {
		if err != nil {
			return status.Errorf(status.Code(err), "destination %s: %s", p.destinations[i].id, status.Convert(err).Message())
		}
	}
	return nil
}

func (p *proxy) wrap(
	req *pb.InitiateUploadRequest, d destination, target *pb.UploadInstructions,
) (*pb.UploadInstructions, error) {
	if target.GetUploadStrategy() != pb.UploadInstructions_UPLOAD_STRATEGY_SIGNED_URL {
		return nil, status.Error(codes.FailedPrecondition, "backend must use signed URLs; byte proxying is unsupported")
	}
	u, err := url.Parse(target.SignedUrl)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		target.UploadId == "" || target.BuildId != req.BuildId || target.Type != req.Type {
		return nil, status.Error(codes.FailedPrecondition, "backend returned invalid or mismatched upload instructions")
	}
	data, err := json.Marshal(route{
		Destination: d.id, Identity: d.identity, UploadID: target.UploadId,
		BuildID: req.BuildId, Type: req.Type, Expires: p.now().Add(defaultRouteTTL).UnixNano(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "cannot encode completion route")
	}
	wrapped := proto.Clone(target).(*pb.UploadInstructions)
	wrapped.UploadId = base64.RawURLEncoding.EncodeToString(data)
	return wrapped, nil
}

// MarkUploadFinished restores the upload ID and calls only the destination that issued the upload.
func (p *proxy) MarkUploadFinished(
	ctx context.Context, req *pb.MarkUploadFinishedRequest,
) (*pb.MarkUploadFinishedResponse, error) {
	if len(p.destinations) == 1 {
		d := p.destinations[0]
		return d.client.MarkUploadFinished(p.downstream(ctx, d), req)
	}
	data, err := base64.RawURLEncoding.DecodeString(req.UploadId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid completion token")
	}
	var route route
	if err := json.Unmarshal(data, &route); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid completion route")
	}
	if route.UploadID == "" || route.BuildID != req.BuildId || route.Type != req.Type {
		return nil, status.Error(codes.InvalidArgument, "invalid completion route or artifact binding")
	}
	if p.now().UnixNano() >= route.Expires {
		return nil, status.Error(codes.FailedPrecondition, "completion token expired; renegotiate the upload")
	}
	for _, d := range p.destinations {
		if d.id != route.Destination || d.identity != route.Identity {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		select {
		case p.slots <- struct{}{}:
			defer func() { <-p.slots }()
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return d.client.MarkUploadFinished(p.downstream(ctx, d), &pb.MarkUploadFinishedRequest{
			BuildId: req.BuildId, Type: req.Type, UploadId: route.UploadID,
		})
	}
	return nil, status.Error(codes.FailedPrecondition, "completion destination was removed or replaced")
}

// Upload forwards artifact bytes to a single upstream; fanout requires direct signed-URL uploads.
func (p *proxy) Upload(stream pb.DebuginfoService_UploadServer) error {
	if len(p.destinations) != 1 {
		return status.Error(codes.Unimplemented, "artifact bytes must go directly to signed URLs")
	}
	d := p.destinations[0]
	ctx, cancel := context.WithCancel(p.downstream(stream.Context(), d))
	defer cancel()
	upstream, err := d.client.Upload(ctx)
	if err != nil {
		return err
	}
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := upstream.Send(message); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}
	response, err := upstream.CloseAndRecv()
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

func destinationIdentity(exporter exporterConfig) string {
	data, _ := json.Marshal(exporter)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

var _ pb.DebuginfoServiceServer = (*proxy)(nil)
