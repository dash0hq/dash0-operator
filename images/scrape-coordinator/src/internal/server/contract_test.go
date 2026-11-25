// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	prometheusgoclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	promdiscovery "github.com/prometheus/prometheus/discovery"
	promhttp "github.com/prometheus/prometheus/discovery/http"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The contract tests consume the server's endpoints the way the Prometheus receiver of the OpenTelemetry collector does
// (receiver/prometheusreceiver/internal/targetallocator/manager.go): decode /scrape_configs with goccy/go-yaml after
// replacing $(SHARD), validate each scrape config, and fetch the targets with Prometheus' HTTP service discovery.

// receiverScrapeConfigs mirrors getScrapeConfigsResponse and the validation in sync of the receiver.
func receiverScrapeConfigs(
	t *testing.T,
	baseURL string,
	collectorID string,
) (map[string]*promconfig.ScrapeConfig, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/scrape_configs", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	jobToScrapeConfig := map[string]*promconfig.ScrapeConfig{}
	if err := yaml.Unmarshal(bytes.ReplaceAll(body, []byte("$(SHARD)"), []byte("0")), &jobToScrapeConfig); err != nil {
		return nil, err
	}
	globalCfg := promconfig.DefaultGlobalConfig
	for jobName, scrapeConfig := range jobToScrapeConfig {
		httpSD := promhttp.SDConfig{
			RefreshInterval: model.Duration(30 * time.Second),
			URL: fmt.Sprintf("%s/jobs/%s/targets?collector_id=%s", baseURL, url.QueryEscape(jobName),
				collectorID),
		}
		httpSD.HTTPClientConfig.FollowRedirects = false
		scrapeConfig.ServiceDiscoveryConfigs = promdiscovery.Configs{&httpSD}
		if scrapeConfig.ScrapeFallbackProtocol == "" {
			scrapeConfig.ScrapeFallbackProtocol = promconfig.PrometheusText0_0_4
		}
		if err := scrapeConfig.Validate(globalCfg); err != nil {
			return nil, err
		}
	}
	return jobToScrapeConfig, nil
}

// receiverTargetGroups fetches the targets of a scrape config with Prometheus' HTTP service discovery.
func receiverTargetGroups(t *testing.T, scrapeConfig *promconfig.ScrapeConfig) []*targetgroup.Group {
	t.Helper()
	require.Len(t, scrapeConfig.ServiceDiscoveryConfigs, 1)
	sdConfig := scrapeConfig.ServiceDiscoveryConfigs[0].(*promhttp.SDConfig)
	registry := prometheusgoclient.NewRegistry()
	metrics := sdConfig.NewDiscovererMetrics(registry, promdiscovery.NewRefreshMetrics(registry))
	d, err := promhttp.NewDiscovery(sdConfig, promdiscovery.DiscovererOptions{Logger: discardLogger, Metrics: metrics})
	require.NoError(t, err)
	groups, err := d.Refresh(t.Context())
	require.NoError(t, err)
	return groups
}

func TestContractWithThePrometheusReceiver(t *testing.T) {
	jobWithSlashes := "serviceMonitor/ns/app/0"
	s := newTestServer(t, newAllocator(
		newTarget(jobWithSlashes, "10.0.0.1:8080", "__meta_kubernetes_pod_name", "app-1"),
		newTarget(jobWithSlashes, "10.0.0.2:8080", "__meta_kubernetes_pod_name", "app-2"),
	), Options{})

	httpServer := httptest.NewServer(s.plainServer.Handler)
	defer httpServer.Close()

	_, err := receiverScrapeConfigs(t, httpServer.URL, testCollector)
	require.Error(t, err, "the receiver retries as long as the scrape configs are not available")

	scrapeConfig := basicAuthScrapeConfig(t, jobWithSlashes)
	scrapeConfig.RelabelConfigs = append(scrapeConfig.RelabelConfigs,
		&relabel.Config{
			SourceLabels: model.LabelNames{"__address__"},
			Separator:    ";",
			Regex:        relabel.MustNewRegexp("(.*)"),
			Modulus:      1,
			TargetLabel:  "__tmp_hash",
			Replacement:  "$1",
			Action:       relabel.HashMod,
		},
		&relabel.Config{
			SourceLabels: model.LabelNames{"__tmp_hash"},
			Separator:    ";",
			Regex:        relabel.MustNewRegexp("$(SHARD)"),
			Replacement:  "$1",
			Action:       relabel.Keep,
		},
		// The defaults are set as by loading a configuration file.
		&relabel.Config{
			SourceLabels: model.LabelNames{"__meta_kubernetes_pod_name"},
			Separator:    relabel.DefaultRelabelConfig.Separator,
			Regex:        relabel.DefaultRelabelConfig.Regex,
			Replacement:  relabel.DefaultRelabelConfig.Replacement,
			TargetLabel:  "pod",
			Action:       relabel.KeepEqual,
		},
	)
	require.NoError(t, s.SetScrapeConfigs([]*promconfig.ScrapeConfig{scrapeConfig}))

	scrapeConfigs, err := receiverScrapeConfigs(t, httpServer.URL, testCollector)
	require.NoError(t, err)
	require.Contains(t, scrapeConfigs, jobWithSlashes)
	received := scrapeConfigs[jobWithSlashes]
	assert.Equal(t, "<secret>", string(received.HTTPClientConfig.BasicAuth.Password))
	assert.Equal(t, "0", received.RelabelConfigs[2].Regex.String(), "$(SHARD) is replaced by the receiver")

	groups := receiverTargetGroups(t, received)
	require.Len(t, groups, 2)
	addresses := map[string]model.LabelSet{}
	for _, group := range groups {
		require.Len(t, group.Targets, 1)
		addresses[string(group.Targets[0][model.AddressLabel])] = group.Labels
	}
	require.Contains(t, addresses, "10.0.0.1:8080")
	assert.Equal(t, model.LabelValue("app-1"), addresses["10.0.0.1:8080"]["__meta_kubernetes_pod_name"])
	assert.Equal(t, model.LabelValue("node-1"), addresses["10.0.0.1:8080"]["__meta_kubernetes_pod_node_name"])
	require.Contains(t, addresses, "10.0.0.2:8080")

	other := *received
	otherSD := *received.ServiceDiscoveryConfigs[0].(*promhttp.SDConfig)
	otherSD.URL = fmt.Sprintf("%s/jobs/%s/targets?collector_id=unknown", httpServer.URL, url.QueryEscape(jobWithSlashes))
	other.ServiceDiscoveryConfigs = promdiscovery.Configs{&otherSD}
	assert.Empty(t, receiverTargetGroups(t, &other), "an unknown collector gets no targets")
}
