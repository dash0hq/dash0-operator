// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package server serves the scrape configs and the targets of each collector over HTTP, in the format the Prometheus
// receiver of the OpenTelemetry collector expects from a target allocator.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	promcommonconfig "github.com/prometheus/common/config"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/relabel"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/allocation"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
)

const (
	readHeaderTimeout = 30 * time.Second
	shutdownTimeout   = 5 * time.Second

	jobsPathPrefix    = "/jobs/"
	targetsPathSuffix = "/targets"
)

var (
	emptyList      = []byte("[]")
	listStart      = []byte("[")
	listSeparator  = []byte(",")
	listEnd        = []byte("]")
	errJobNotFound = errors.New("the path does not contain a job name")
)

// SnapshotProvider provides the current assignment of targets to collectors.
type SnapshotProvider interface {
	// Snapshot returns the current assignment, or nil as long as the collectors are not known yet.
	Snapshot() *allocation.Snapshot
}

// Options configures a Server.
type Options struct {
	// ListenAddr is the address of the plain HTTP listener.
	ListenAddr string
	// TLS enables a listener with mutual TLS. The plain listener then only serves the health endpoints.
	TLS *config.TLS
	// AllowInsecureAuthSecrets serves the secrets in the scrape configs on the plain listener.
	AllowInsecureAuthSecrets bool
	// DebugEndpoints enables /debug/assignments.
	DebugEndpoints bool
}

type scrapeConfigResponses struct {
	masked   []byte
	unmasked []byte
}

// Server serves the scrape configs and the targets of each collector.
type Server struct {
	logger                   *slog.Logger
	snapshots                SnapshotProvider
	allowInsecureAuthSecrets bool
	scrapeConfigs            atomic.Pointer[scrapeConfigResponses]

	plainServer *http.Server
	tlsServer   *http.Server
	certWatcher *certwatcher.CertWatcher
}

// New creates a Server. With TLS, it reads the certificates and fails if they are not valid.
func New(logger *slog.Logger, snapshots SnapshotProvider, options Options) (*Server, error) {
	s := &Server{
		logger:                   logger,
		snapshots:                snapshots,
		allowInsecureAuthSecrets: options.AllowInsecureAuthSecrets,
	}
	if options.TLS == nil {
		s.plainServer = newHTTPServer(options.ListenAddr, s.dataHandler(options.DebugEndpoints))
		if options.AllowInsecureAuthSecrets {
			logger.Warn("allowInsecureAuthSecrets is enabled, the secrets of the scrape configs are served over " +
				"plain HTTP")
		}
		return s, nil
	}

	tlsConfig, certWatcher, err := newTLSConfig(logger, options.TLS)
	if err != nil {
		return nil, err
	}
	s.certWatcher = certWatcher
	s.plainServer = newHTTPServer(options.ListenAddr, s.healthHandler())
	s.tlsServer = newHTTPServer(options.TLS.ListenAddr, s.dataHandler(options.DebugEndpoints))
	s.tlsServer.TLSConfig = tlsConfig
	return s, nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}

func (s *Server) healthHandler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.serveReadiness)
	return mux
}

func (s *Server) dataHandler(debugEndpoints bool) *http.ServeMux {
	mux := s.healthHandler()
	mux.HandleFunc("GET /scrape_configs", s.serveScrapeConfigs)
	mux.HandleFunc("GET /jobs/{job}/targets", s.serveTargets)
	if debugEndpoints {
		mux.HandleFunc("GET /debug/assignments", s.serveAssignments)
	}
	return mux
}

// Run serves the listeners until ctx is cancelled, and then shuts them down.
func (s *Server) Run(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		s.logger.Info("serving HTTP", "address", s.plainServer.Addr)
		return ignoreServerClosed(s.plainServer.ListenAndServe())
	})
	if s.tlsServer != nil {
		group.Go(func() error {
			s.logger.Info("serving HTTPS", "address", s.tlsServer.Addr)
			return ignoreServerClosed(s.tlsServer.ListenAndServeTLS("", ""))
		})
		group.Go(func() error { return s.certWatcher.Start(groupCtx) })
	}
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		err := s.plainServer.Shutdown(shutdownCtx)
		if s.tlsServer != nil {
			err = errors.Join(err, s.tlsServer.Shutdown(shutdownCtx))
		}
		return err
	})
	return group.Wait()
}

func ignoreServerClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// SetScrapeConfigs replaces the served scrape configs. It switches the global MarshalSecretValue of the Prometheus
// libraries while marshalling, so no other Prometheus configuration must be marshalled concurrently.
func (s *Server) SetScrapeConfigs(scrapeConfigs []*promconfig.ScrapeConfig) error {
	defer func() { promcommonconfig.MarshalSecretValue = false }()
	byJob := make(map[string]*promconfig.ScrapeConfig, len(scrapeConfigs))
	for _, scrapeConfig := range scrapeConfigs {
		byJob[scrapeConfig.JobName] = scrapeConfig
	}
	promcommonconfig.MarshalSecretValue = false
	masked, err := marshalAsJSON(byJob)
	if err != nil {
		return err
	}
	promcommonconfig.MarshalSecretValue = true
	unmasked, err := marshalAsJSON(byJob)
	if err != nil {
		return err
	}
	s.scrapeConfigs.Store(&scrapeConfigResponses{masked: masked, unmasked: unmasked})
	return nil
}

// marshalAsJSON marshals to YAML first, where the marshalling of the Prometheus configuration types applies.
func marshalAsJSON(byJob map[string]*promconfig.ScrapeConfig) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(byJob)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal the scrape configs: %w", err)
	}
	jsonBytes, err := sigsyaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return nil, fmt.Errorf("cannot convert the scrape configs to JSON: %w", err)
	}
	return withoutRegexOfEqualActions(jsonBytes)
}

// withoutRegexOfEqualActions removes the regex of keepequal and dropequal relabel configs. A parsed relabel config
// always has a regex, which is marshalled even when it is cleared, but the Prometheus receiver rejects one for these
// actions.
func withoutRegexOfEqualActions(jsonBytes []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()
	byJob := map[string]map[string]any{}
	if err := decoder.Decode(&byJob); err != nil {
		return nil, fmt.Errorf("cannot decode the scrape configs: %w", err)
	}
	for _, scrapeConfig := range byJob {
		for _, key := range []string{"relabel_configs", "metric_relabel_configs"} {
			relabelConfigs, _ := scrapeConfig[key].([]any)
			for _, item := range relabelConfigs {
				relabelConfig, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if action := relabelConfig["action"]; action == string(relabel.KeepEqual) ||
					action == string(relabel.DropEqual) {
					delete(relabelConfig, "regex")
				}
			}
		}
	}
	return json.Marshal(byJob)
}

func (s *Server) serveScrapeConfigs(w http.ResponseWriter, r *http.Request) {
	responses := s.scrapeConfigs.Load()
	if responses == nil {
		http.Error(w, "the scrape configs are not available yet", http.StatusServiceUnavailable)
		return
	}
	body := responses.masked
	if r.TLS != nil || s.allowInsecureAuthSecrets {
		body = responses.unmasked
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		s.logger.Debug("cannot write the scrape configs", "error", err)
	}
}

// serveTargets serves the targets of a job that are assigned to a collector, as a JSON list in the format of the
// Prometheus HTTP service discovery. The list is empty for an unknown job or collector.
func (s *Server) serveTargets(w http.ResponseWriter, r *http.Request) {
	collector := r.URL.Query().Get("collector_id")
	if collector == "" {
		http.Error(w, "the query parameter collector_id is missing", http.StatusBadRequest)
		return
	}
	job, err := jobFromPath(r.URL.EscapedPath())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var fragments [][]byte
	if snapshot := s.snapshots.Snapshot(); snapshot != nil {
		fragments = snapshot.TargetFragments(collector, job)
	}
	w.Header().Set("Content-Type", "application/json")
	if len(fragments) == 0 {
		_, _ = w.Write(emptyList)
		return
	}
	response := make(net.Buffers, 0, 2*len(fragments)+1)
	response = append(response, listStart)
	for i, fragment := range fragments {
		if i > 0 {
			response = append(response, listSeparator)
		}
		response = append(response, fragment)
	}
	response = append(response, listEnd)
	if _, err := response.WriteTo(w); err != nil {
		s.logger.Debug("cannot write the targets", "job", job, "collector", collector, "error", err)
	}
}

// jobFromPath returns the job name of a targets path. The Prometheus receiver escapes the job name with
// url.QueryEscape, so it is unescaped from the raw path; the path value of the request would keep a "+" for a space.
func jobFromPath(escapedPath string) (string, error) {
	escapedJob := strings.TrimSuffix(strings.TrimPrefix(escapedPath, jobsPathPrefix), targetsPathSuffix)
	if escapedJob == "" {
		return "", errJobNotFound
	}
	return url.QueryUnescape(escapedJob)
}

func (s *Server) serveAssignments(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.snapshots.Snapshot()
	if snapshot == nil {
		http.Error(w, "the collectors are not known yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot.Assignments()); err != nil {
		s.logger.Debug("cannot write the assignments", "error", err)
	}
}

// serveReadiness reports ready once the scrape configs and the collectors are known, even if there are no
// collectors. It must not wait for the collectors to be ready, because they wait for the scrape configs.
func (s *Server) serveReadiness(w http.ResponseWriter, _ *http.Request) {
	if s.scrapeConfigs.Load() == nil || s.snapshots.Snapshot() == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
