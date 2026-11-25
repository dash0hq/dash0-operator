// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/allocation"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/collectors"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/targets"
)

const (
	testCollector = "collector-1"
	testNode      = "node-1"
)

var discardLogger = slog.New(slog.DiscardHandler)

func newTarget(job string, address string, extraLabels ...string) *targets.Target {
	ls := append([]string{"__address__", address, "__meta_kubernetes_pod_node_name", testNode}, extraLabels...)
	return targets.NewTarget(job, labels.FromStrings(ls...))
}

// newAllocator returns an allocator that assigns all given targets to testCollector.
func newAllocator(ts ...*targets.Target) *allocation.Allocator {
	allocator := allocation.New(discardLogger, noop.NewMeterProvider().Meter("test"), []string{allocation.PoolPerNode})
	allocator.SetCollectors([]*collectors.Collector{
		{Name: testCollector, NodeName: testNode, Pool: allocation.PoolPerNode},
	})
	allocator.SetTargets(ts)
	return allocator
}

// basicAuthScrapeConfig returns a scrape config as the scrape config generator produces it, with the defaults set by
// parsing.
func basicAuthScrapeConfig(t *testing.T, job string) *promconfig.ScrapeConfig {
	t.Helper()
	cfg, err := promconfig.Load(fmt.Sprintf(`
scrape_configs:
  - job_name: %q
    basic_auth:
      username: user
      password: secret-password
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: pod
        action: keepequal
    metric_relabel_configs:
      - source_labels: [__name__]
        target_label: name
        action: dropequal
    static_configs:
      - targets: ["unused:8080"]
`, job), discardLogger)
	require.NoError(t, err)
	require.Len(t, cfg.ScrapeConfigs, 1)
	return cfg.ScrapeConfigs[0]
}

func newTestServer(t *testing.T, snapshots SnapshotProvider, options Options) *Server {
	t.Helper()
	s, err := New(discardLogger, snapshots, options)
	require.NoError(t, err)
	return s
}

func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	return recorder
}

func decodeScrapeConfigs(t *testing.T, body []byte) map[string]map[string]any {
	t.Helper()
	var result map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &result))
	return result
}

func basicAuthPassword(scrapeConfig map[string]any) any {
	return scrapeConfig["basic_auth"].(map[string]any)["password"]
}

func TestScrapeConfigsAreMaskedOnThePlainListener(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		allowInsecureAuthSecrets bool
		wantPassword             string
	}{
		{name: "masked by default", wantPassword: "<secret>"},
		{name: "unmasked with allowInsecureAuthSecrets", allowInsecureAuthSecrets: true, wantPassword: "secret-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, newAllocator(), Options{AllowInsecureAuthSecrets: tc.allowInsecureAuthSecrets})
			require.NoError(t, s.SetScrapeConfigs([]*promconfig.ScrapeConfig{basicAuthScrapeConfig(t, "job")}))

			response := get(t, s.plainServer.Handler, "/scrape_configs")
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			assert.Equal(t, tc.wantPassword, basicAuthPassword(decodeScrapeConfigs(t, response.Body.Bytes())["job"]))
		})
	}
}

func TestScrapeConfigsHaveNoRegexForEqualActions(t *testing.T) {
	s := newTestServer(t, newAllocator(), Options{})
	scrapeConfig := basicAuthScrapeConfig(t, "job")
	require.NoError(t, s.SetScrapeConfigs([]*promconfig.ScrapeConfig{scrapeConfig}))

	served := decodeScrapeConfigs(t, get(t, s.plainServer.Handler, "/scrape_configs").Body.Bytes())["job"]
	for _, key := range []string{"relabel_configs", "metric_relabel_configs"} {
		relabelConfigs := served[key].([]any)
		require.Len(t, relabelConfigs, 1)
		assert.NotContains(t, relabelConfigs[0], "regex", key)
	}
	assert.NotNil(t, scrapeConfig.RelabelConfigs[0].Regex.Regexp, "the scrape config itself is not modified")
}

func TestScrapeConfigsAreUnavailableBeforeTheFirstGeneration(t *testing.T) {
	s := newTestServer(t, newAllocator(), Options{})
	assert.Equal(t, http.StatusServiceUnavailable, get(t, s.plainServer.Handler, "/scrape_configs").Code)
}

func TestTargets(t *testing.T) {
	jobWithSpecialCharacters := "serviceMonitor/ns/a b+c/0"
	s := newTestServer(t, newAllocator(
		newTarget("job", "10.0.0.1:8080"),
		newTarget("job", "10.0.0.2:8080"),
		newTarget(jobWithSpecialCharacters, "10.0.0.3:8080"),
	), Options{})

	for _, tc := range []struct {
		name          string
		target        string
		wantCode      int
		wantAddresses []string
	}{
		{
			name:          "targets of the job and collector",
			target:        "/jobs/job/targets?collector_id=" + testCollector,
			wantCode:      http.StatusOK,
			wantAddresses: []string{"10.0.0.1:8080", "10.0.0.2:8080"},
		},
		{
			name: "job name escaped like the Prometheus receiver does",
			target: fmt.Sprintf("/jobs/%s/targets?collector_id=%s",
				url.QueryEscape(jobWithSpecialCharacters), testCollector),
			wantCode:      http.StatusOK,
			wantAddresses: []string{"10.0.0.3:8080"},
		},
		{
			name:          "unknown job",
			target:        "/jobs/unknown/targets?collector_id=" + testCollector,
			wantCode:      http.StatusOK,
			wantAddresses: []string{},
		},
		{
			name:          "unknown collector",
			target:        "/jobs/job/targets?collector_id=unknown",
			wantCode:      http.StatusOK,
			wantAddresses: []string{},
		},
		{
			name:     "missing collector",
			target:   "/jobs/job/targets",
			wantCode: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := get(t, s.plainServer.Handler, tc.target)
			require.Equal(t, tc.wantCode, response.Code)
			if tc.wantAddresses == nil {
				return
			}
			var groups []struct {
				Targets []string          `json:"targets"`
				Labels  map[string]string `json:"labels"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &groups))
			addresses := []string{}
			for _, group := range groups {
				addresses = append(addresses, group.Targets...)
				assert.Equal(t, testNode, group.Labels["__meta_kubernetes_pod_node_name"])
			}
			assert.ElementsMatch(t, tc.wantAddresses, addresses)
		})
	}
}

type noSnapshots struct{}

func (noSnapshots) Snapshot() *allocation.Snapshot { return nil }

func TestHealthEndpoints(t *testing.T) {
	notReady := newTestServer(t, noSnapshots{}, Options{})
	require.NoError(t, notReady.SetScrapeConfigs(nil))
	assert.Equal(t, http.StatusOK, get(t, notReady.plainServer.Handler, "/livez").Code)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, notReady.plainServer.Handler, "/readyz").Code,
		"not ready without the collectors")

	s := newTestServer(t, newAllocator(), Options{})
	assert.Equal(t, http.StatusServiceUnavailable, get(t, s.plainServer.Handler, "/readyz").Code,
		"not ready without the scrape configs")
	require.NoError(t, s.SetScrapeConfigs(nil))
	assert.Equal(t, http.StatusOK, get(t, s.plainServer.Handler, "/readyz").Code)
}

func TestDebugEndpoints(t *testing.T) {
	s := newTestServer(t, newAllocator(newTarget("job", "10.0.0.1:8080")), Options{DebugEndpoints: true})
	response := get(t, s.plainServer.Handler, "/debug/assignments")
	require.Equal(t, http.StatusOK, response.Code)
	var assignments allocation.Assignments
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &assignments))
	assert.Equal(t, map[string]map[string]int{testCollector: {"job": 1}}, assignments.Collectors)

	withoutDebugEndpoints := newTestServer(t, newAllocator(), Options{})
	assert.Equal(t, http.StatusNotFound, get(t, withoutDebugEndpoints.plainServer.Handler, "/debug/assignments").Code)
}

func TestNoScrapeConfigsAreAnEmptyObject(t *testing.T) {
	s := newTestServer(t, newAllocator(), Options{})
	require.NoError(t, s.SetScrapeConfigs(nil))
	response := get(t, s.plainServer.Handler, "/scrape_configs")
	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, "{}", response.Body.String())
}
