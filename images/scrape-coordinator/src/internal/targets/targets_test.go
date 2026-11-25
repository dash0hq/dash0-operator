// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targets

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	promdiscovery "github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var discardLogger = slog.New(slog.DiscardHandler)

// loadScrapeConfigs parses scrape configs the way the scrape config generator does, so the defaults are set.
func loadScrapeConfigs(t *testing.T, scrapeConfigsYAML string) []*promconfig.ScrapeConfig {
	t.Helper()
	cfg, err := promconfig.Load("scrape_configs:\n"+scrapeConfigsYAML, discardLogger)
	require.NoError(t, err)
	return cfg.ScrapeConfigs
}

// staticGroups returns the target groups of the static configs, as service discovery reports them.
func staticGroups(scrapeConfigs []*promconfig.ScrapeConfig) map[string][]*targetgroup.Group {
	groups := map[string][]*targetgroup.Group{}
	for _, scrapeConfig := range scrapeConfigs {
		for _, sdConfig := range scrapeConfig.ServiceDiscoveryConfigs {
			if staticConfig, ok := sdConfig.(promdiscovery.StaticConfig); ok {
				groups[scrapeConfig.JobName] = append(groups[scrapeConfig.JobName], staticConfig...)
			}
		}
	}
	return groups
}

func build(t *testing.T, scrapeConfigsYAML string) []*Target {
	t.Helper()
	scrapeConfigs := loadScrapeConfigs(t, scrapeConfigsYAML)
	builder := NewBuilder(discardLogger, time.Hour)
	builder.SetScrapeConfigs(scrapeConfigs)
	builder.groups = staticGroups(scrapeConfigs)
	return builder.Build()
}

func addresses(ts []*Target) []string {
	result := make([]string, 0, len(ts))
	for _, target := range ts {
		result = append(result, target.Address)
	}
	sort.Strings(result)
	return result
}

// threeApps is a static config with three targets that differ in the label app.
const threeApps = `
    static_configs:
      - targets: ["a:8080"]
        labels: {app: a, expected: a}
      - targets: ["b:8080"]
        labels: {app: b, expected: x}
      - targets: ["c:8080"]
        labels: {app: C, expected: x}
`

func TestRelabelingDecidesWhichTargetsAreKept(t *testing.T) {
	for _, tc := range []struct {
		name           string
		relabelConfigs string
		want           []string
	}{
		{
			name:           "no relabel configs",
			relabelConfigs: "[]",
			want:           []string{"a:8080", "b:8080", "c:8080"},
		},
		{
			name:           "keep",
			relabelConfigs: "[{source_labels: [app], regex: a, action: keep}]",
			want:           []string{"a:8080"},
		},
		{
			name:           "drop",
			relabelConfigs: "[{source_labels: [app], regex: a, action: drop}]",
			want:           []string{"b:8080", "c:8080"},
		},
		{
			name:           "keepequal",
			relabelConfigs: "[{source_labels: [app], target_label: expected, action: keepequal}]",
			want:           []string{"a:8080"},
		},
		{
			name:           "dropequal",
			relabelConfigs: "[{source_labels: [app], target_label: expected, action: dropequal}]",
			want:           []string{"b:8080", "c:8080"},
		},
		{
			name: "lowercase before keep",
			relabelConfigs: `
      - {source_labels: [app], target_label: app, action: lowercase}
      - {source_labels: [app], regex: c, action: keep}`,
			want: []string{"c:8080"},
		},
		{
			name: "labelmap before keep",
			relabelConfigs: `
      - {regex: (app), replacement: mapped_$1, action: labelmap}
      - {source_labels: [mapped_app], regex: b, action: keep}`,
			want: []string{"b:8080"},
		},
		{
			name: "labeldrop before keep",
			relabelConfigs: `
      - {regex: app, action: labeldrop}
      - {source_labels: [app], regex: ".+", action: keep}`,
			want: []string{},
		},
		{
			name: "sharding rules of the Prometheus operator with the shard of the collectors",
			relabelConfigs: `
      - {source_labels: [__address__, __tmp_hash], target_label: __tmp_hash, regex: (.+);, replacement: $1}
      - {source_labels: [__tmp_hash], target_label: __tmp_hash, modulus: 1, action: hashmod}
      - {source_labels: [__tmp_hash, __tmp_disable_sharding], regex: 0;|.+;.+, action: keep}`,
			want: []string{"a:8080", "b:8080", "c:8080"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := build(t, fmt.Sprintf(`
  - job_name: job
    relabel_configs: %s
%s`, tc.relabelConfigs, threeApps))
			assert.Equal(t, tc.want, addresses(ts))
		})
	}
}

func TestRelabelingSeesTheLabelsOfTheScrapeConfig(t *testing.T) {
	for _, tc := range []struct {
		name         string
		sourceLabel  string
		regex        string
		wantDropped  bool
		extraSetting string
	}{
		{name: "job", sourceLabel: "job", regex: "my-job"},
		{name: "scheme", sourceLabel: "__scheme__", regex: "https", extraSetting: "scheme: https"},
		{name: "metrics path", sourceLabel: "__metrics_path__", regex: "/custom", extraSetting: "metrics_path: /custom"},
		{name: "param", sourceLabel: "__param_module", regex: "http_2xx", extraSetting: "params: {module: [http_2xx]}"},
		{name: "a value that does not match", sourceLabel: "job", regex: "other-job", wantDropped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := build(t, fmt.Sprintf(`
  - job_name: my-job
    %s
    relabel_configs: [{source_labels: [%s], regex: %q, action: keep}]
    static_configs: [{targets: ["a:8080"]}]
`, tc.extraSetting, tc.sourceLabel, tc.regex))
			if tc.wantDropped {
				assert.Empty(t, ts)
				return
			}
			require.Len(t, ts, 1)
			assert.Equal(t, labels.FromStrings("__address__", "a:8080"), ts[0].Labels,
				"the labels of the scrape config are not served, the collector adds them itself")
		})
	}
}

func TestServedLabels(t *testing.T) {
	scrapeConfigs := loadScrapeConfigs(t, `
  - job_name: job
    relabel_configs:
      - {source_labels: [__address__], target_label: __address__, replacement: rewritten:9090}
`)
	builder := NewBuilder(discardLogger, time.Hour)
	builder.SetScrapeConfigs(scrapeConfigs)
	builder.groups = map[string][]*targetgroup.Group{"job": {{
		Labels: model.LabelSet{"__meta_group": "group", "shared": "from-group", "multiline": "a\nb"},
		Targets: []model.LabelSet{{
			"__address__":                     "10.0.0.1:8080",
			"shared":                          "from-target",
			"__meta_kubernetes_pod_node_name": "node-1",
			"utf8":                            "Grüße 🚀",
		}},
	}}}

	ts := builder.Build()
	require.Len(t, ts, 1)
	assert.Equal(t, &Target{
		Job:     "job",
		Address: "10.0.0.1:8080",
		Labels: labels.FromStrings(
			"__address__", "10.0.0.1:8080",
			"__meta_group", "group",
			"__meta_kubernetes_pod_node_name", "node-1",
			"multiline", "a\nb",
			"shared", "from-target",
			"utf8", "Grüße 🚀",
		),
		NodeName: "node-1",
	}, ts[0], "the target labels take precedence, the address is the one before relabeling")
}

func TestInvalidTargetsAreSkipped(t *testing.T) {
	ts := build(t, `
  - job_name: job
    relabel_configs:
      - {source_labels: [app], regex: b, target_label: __address__, replacement: "in/valid:8080"}
`+threeApps)
	assert.Equal(t, []string{"a:8080", "c:8080"}, addresses(ts))
}

func TestTargetsThatAreIdenticalAfterRelabelingAreScrapedOnce(t *testing.T) {
	scrapeConfigs := loadScrapeConfigs(t, `
  - job_name: job
`)
	group := func(version string) *targetgroup.Group {
		return &targetgroup.Group{Targets: []model.LabelSet{
			{"__address__": "10.0.0.1:8080", "__meta_version": model.LabelValue(version)},
		}}
	}
	for _, order := range [][]string{{"1", "2"}, {"2", "1"}} {
		builder := NewBuilder(discardLogger, time.Hour)
		builder.SetScrapeConfigs(scrapeConfigs)
		builder.groups = map[string][]*targetgroup.Group{"job": {group(order[0]), group(order[1])}}
		ts := builder.Build()
		require.Len(t, ts, 1)
		assert.Equal(t, "1", ts[0].Labels.Get("__meta_version"), "the smallest label set wins regardless of the order")
	}

	ts := build(t, `
  - job_name: job-a
    static_configs: [{targets: ["10.0.0.1:8080"]}]
  - job_name: job-b
    static_configs: [{targets: ["10.0.0.1:8080"]}]
`)
	assert.Len(t, ts, 2, "the same address in different jobs is scraped per job")

	ts = build(t, `
  - job_name: job
    relabel_configs: [{source_labels: [path], target_label: __metrics_path__}]
    static_configs:
      - targets: ["10.0.0.1:8080"]
        labels: {path: /a}
      - targets: ["10.0.0.1:8080"]
        labels: {path: /b}
`)
	assert.Len(t, ts, 2, "the same address with a different path is scraped twice")
}

func TestGroupsOfRemovedJobsAreIgnored(t *testing.T) {
	scrapeConfigs := loadScrapeConfigs(t, `
  - job_name: job-a
    static_configs: [{targets: ["a:8080"]}]
  - job_name: job-b
    static_configs: [{targets: ["b:8080"]}]
`)
	builder := NewBuilder(discardLogger, time.Hour)
	builder.SetScrapeConfigs(scrapeConfigs)
	builder.groups = staticGroups(scrapeConfigs)
	builder.SetScrapeConfigs(scrapeConfigs[:1])
	assert.Equal(t, []string{"a:8080"}, addresses(builder.Build()))
}

func TestNodeName(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   string
	}{
		{name: "pod", labels: []string{"__meta_kubernetes_pod_node_name", "node-1"}, want: "node-1"},
		{name: "node role", labels: []string{"__meta_kubernetes_node_name", "node-1"}, want: "node-1"},
		{name: "endpoints", labels: []string{"__meta_kubernetes_endpoint_node_name", "node-1"}, want: "node-1"},
		{
			name:   "endpoint slice endpoint",
			labels: []string{"__meta_kubernetes_endpointslice_endpoint_node_name", "node-1"},
			want:   "node-1",
		},
		{
			name: "pod takes precedence over the endpoint slice endpoint",
			labels: []string{
				"__meta_kubernetes_endpointslice_endpoint_node_name", "node-2",
				"__meta_kubernetes_pod_node_name", "node-1",
			},
			want: "node-1",
		},
		{
			name: "endpoint slice address target of kind Node",
			labels: []string{
				"__meta_kubernetes_endpointslice_address_target_kind", "Node",
				"__meta_kubernetes_endpointslice_address_target_name", "node-1",
			},
			want: "node-1",
		},
		{
			name: "endpoint slice address target of another kind",
			labels: []string{
				"__meta_kubernetes_endpointslice_address_target_kind", "Pod",
				"__meta_kubernetes_endpointslice_address_target_name", "pod-1",
			},
		},
		{name: "no node", labels: []string{"__meta_kubernetes_namespace", "namespace"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := NewTarget("job", labels.FromStrings(append([]string{"__address__", "a:8080"}, tc.labels...)...))
			assert.Equal(t, tc.want, target.NodeName)
		})
	}
}

func startBuilder(
	t *testing.T,
	discoveryTimeout time.Duration,
) (*Builder, chan<- map[string][]*targetgroup.Group, <-chan []*Target) {
	t.Helper()
	builder := NewBuilder(discardLogger, 10*time.Millisecond)
	builder.discoveryTimeout = discoveryTimeout
	updates := make(chan map[string][]*targetgroup.Group)
	publications := make(chan []*Target, 10)
	go builder.Run(t.Context(), updates, func(ts []*Target) { publications <- ts })
	return builder, updates, publications
}

func TestFirstTargetsArePublishedOnceServiceDiscoveryHasReported(t *testing.T) {
	builder, updates, publications := startBuilder(t, time.Minute)
	builder.SetScrapeConfigs(loadScrapeConfigs(t, `
  - job_name: job
`))
	select {
	case <-publications:
		require.FailNow(t, "the targets are published before service discovery has reported")
	case <-time.After(200 * time.Millisecond):
	}

	updates <- map[string][]*targetgroup.Group{"job": {{Targets: []model.LabelSet{{"__address__": "a:8080"}}}}}
	select {
	case ts := <-publications:
		assert.Equal(t, []string{"a:8080"}, addresses(ts))
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the targets are not published")
	}
}

func TestTargetsArePublishedWithoutJobs(t *testing.T) {
	builder, _, publications := startBuilder(t, time.Minute)
	builder.SetScrapeConfigs(nil)
	select {
	case ts := <-publications:
		assert.Empty(t, ts)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "service discovery reports nothing without jobs, the empty targets must be published anyway")
	}
}

func TestTargetsArePublishedAfterTheDiscoveryTimeout(t *testing.T) {
	builder, _, publications := startBuilder(t, 300*time.Millisecond)
	time.Sleep(400 * time.Millisecond)
	builder.SetScrapeConfigs(loadScrapeConfigs(t, `
  - job_name: job
`))
	select {
	case <-publications:
		require.FailNow(t, "the timeout starts with the first jobs, not with Run")
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case ts := <-publications:
		assert.Empty(t, ts)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the targets are not published after the discovery timeout")
	}
}

// TestWithTheDiscoveryManager runs the builder against Prometheus' discovery manager, as the coordinator does.
func TestWithTheDiscoveryManager(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := prometheus.NewRegistry()
	sdMetrics, err := promdiscovery.CreateAndRegisterSDMetrics(registry)
	require.NoError(t, err)
	manager := promdiscovery.NewManager(ctx, discardLogger, registry, sdMetrics,
		promdiscovery.Updatert(10*time.Millisecond))
	require.NotNil(t, manager)
	go func() { _ = manager.Run() }()

	builder := NewBuilder(discardLogger, 10*time.Millisecond)
	var mu sync.Mutex
	var published []*Target
	go builder.Run(ctx, manager.SyncCh(), func(ts []*Target) {
		mu.Lock()
		published = ts
		mu.Unlock()
	})
	latest := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return addresses(published)
	}

	apply := func(scrapeConfigsYAML string) {
		scrapeConfigs := loadScrapeConfigs(t, scrapeConfigsYAML)
		builder.SetScrapeConfigs(scrapeConfigs)
		sdConfigs := map[string]promdiscovery.Configs{}
		for _, scrapeConfig := range scrapeConfigs {
			sdConfigs[scrapeConfig.JobName] = scrapeConfig.ServiceDiscoveryConfigs
		}
		require.NoError(t, manager.ApplyConfig(sdConfigs))
	}

	apply(`
  - job_name: job
    relabel_configs: [{source_labels: [app], regex: a|b, action: keep}]
` + threeApps)
	require.Eventually(t, func() bool {
		return strings.Join(latest(), ",") == "a:8080,b:8080"
	}, 5*time.Second, 10*time.Millisecond)

	apply(`
  - job_name: job
    relabel_configs: [{source_labels: [app], regex: b, action: keep}]
` + threeApps)
	require.Eventually(t, func() bool {
		return strings.Join(latest(), ",") == "b:8080"
	}, 5*time.Second, 10*time.Millisecond, "a changed relabel config applies without a change of the targets")
}

func BenchmarkBuild(b *testing.B) {
	scrapeConfigs, err := promconfig.Load(`
scrape_configs:
  - job_name: job
    relabel_configs: [{source_labels: [__meta_kubernetes_pod_label_app], regex: keep, action: keep}]
`, discardLogger)
	require.NoError(b, err)
	group := &targetgroup.Group{Labels: model.LabelSet{"__meta_kubernetes_namespace": "namespace"}}
	for i := range 10_000 {
		app := "drop"
		if i%10 == 0 {
			app = "keep"
		}
		target := model.LabelSet{
			"__address__":                     model.LabelValue(fmt.Sprintf("10.0.%d.%d:8080", i/256, i%256)),
			"__meta_kubernetes_pod_label_app": model.LabelValue(app),
			"__meta_kubernetes_pod_node_name": "node",
			"__meta_kubernetes_pod_name":      model.LabelValue(fmt.Sprintf("pod-%d", i)),
		}
		for j := range 30 {
			target[model.LabelName(fmt.Sprintf("__meta_kubernetes_pod_annotation_%d", j))] = "value"
		}
		group.Targets = append(group.Targets, target)
	}
	builder := NewBuilder(discardLogger, time.Hour)
	builder.SetScrapeConfigs(scrapeConfigs.ScrapeConfigs)
	builder.groups = map[string][]*targetgroup.Group{"job": {group}}
	b.ReportAllocs()
	for b.Loop() {
		if len(builder.Build()) != 1_000 {
			b.Fatal("unexpected number of targets")
		}
	}
}
