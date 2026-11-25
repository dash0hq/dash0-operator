// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/collectors"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/targets"
)

var (
	discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	t0            = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func newTarget(job string, address string, nodeName string, extraLabels ...string) *targets.Target {
	ls := []string{"__address__", address}
	if nodeName != "" {
		ls = append(ls, "__meta_kubernetes_pod_node_name", nodeName)
	}
	ls = append(ls, extraLabels...)
	return targets.NewTarget(job, labels.FromStrings(ls...))
}

func newCollector(name string, nodeName string, created time.Time) *collectors.Collector {
	return &collectors.Collector{Name: name, NodeName: nodeName, Pool: PoolPerNode, CreationTimestamp: created}
}

func newTestAllocator() *Allocator {
	return New(discardLogger, noop.NewMeterProvider().Meter("test"), []string{PoolPerNode})
}

type decodedTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func decode(t *testing.T, fragments [][]byte) []decodedTarget {
	t.Helper()
	result := make([]decodedTarget, 0, len(fragments))
	for _, fragment := range fragments {
		var target decodedTarget
		require.NoError(t, json.Unmarshal(fragment, &target))
		result = append(result, target)
	}
	return result
}

func addresses(t *testing.T, fragments [][]byte) []string {
	t.Helper()
	var result []string
	for _, target := range decode(t, fragments) {
		result = append(result, target.Targets...)
	}
	return result
}

func TestNoSnapshotBeforeCollectorsAreReceived(t *testing.T) {
	a := newTestAllocator()
	a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1")})
	assert.Nil(t, a.Snapshot())

	a.SetCollectors(nil)
	snapshot := a.Snapshot()
	require.NotNil(t, snapshot)
	assert.Equal(t, map[string]int{ReasonNoCollectorOnNode: 1}, snapshot.Assignments().Unassigned)
}

func TestNoSnapshotBeforeTargetsAreReceived(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	assert.Nil(t, a.Snapshot())

	a.SetTargets(nil)
	require.NotNil(t, a.Snapshot())
}

func TestRoute(t *testing.T) {
	perNode := map[string]picker{PoolPerNode: newPerNodePicker(nil)}
	for _, tc := range []struct {
		name     string
		target   *targets.Target
		pickers  map[string]picker
		wantPool string
		wantOk   bool
	}{
		{
			name:     "target with a node goes to the per-node pool",
			target:   newTarget("job", "10.0.0.1:8080", "node-1"),
			pickers:  perNode,
			wantPool: PoolPerNode,
			wantOk:   true,
		},
		{
			name:    "target without a node has no pool",
			target:  newTarget("job", "10.0.0.1:8080", ""),
			pickers: perNode,
		},
		{
			name:   "target with a node has no pool without a per-node pool",
			target: newTarget("job", "10.0.0.1:8080", "node-1"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, ok := route(tc.target, tc.pickers)
			assert.Equal(t, tc.wantPool, pool)
			assert.Equal(t, tc.wantOk, ok)
		})
	}
}

func TestAssignsTargetsPerNode(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{
		newCollector("collector-1", "node-1", t0),
		newCollector("collector-2", "node-2", t0),
	})
	a.SetTargets([]*targets.Target{
		newTarget("job-a", "10.0.0.1:8080", "node-1"),
		newTarget("job-a", "10.0.0.2:8080", "node-2"),
		newTarget("job-b", "10.0.0.3:8080", "node-1"),
		newTarget("job-b", "10.0.0.4:8080", "node-3"),
		newTarget("job-b", "10.0.0.5:8080", ""),
	})

	snapshot := a.Snapshot()
	assert.Equal(t, []string{"10.0.0.1:8080"}, addresses(t, snapshot.TargetFragments("collector-1", "job-a")))
	assert.Equal(t, []string{"10.0.0.3:8080"}, addresses(t, snapshot.TargetFragments("collector-1", "job-b")))
	assert.Equal(t, []string{"10.0.0.2:8080"}, addresses(t, snapshot.TargetFragments("collector-2", "job-a")))
	assert.Empty(t, snapshot.TargetFragments("collector-2", "job-b"))
	assert.Empty(t, snapshot.TargetFragments("unknown", "job-a"))
	assert.Empty(t, snapshot.TargetFragments("collector-1", "unknown"))
	assert.Equal(t, Assignments{
		Targets: 5,
		Pools:   map[string]int{PoolPerNode: 3},
		Collectors: map[string]map[string]int{
			"collector-1": {"job-a": 1, "job-b": 1},
			"collector-2": {"job-a": 1},
		},
		Unassigned: map[string]int{ReasonNoCollectorOnNode: 1, ReasonNoPool: 1},
	}, snapshot.Assignments())
}

func TestFragmentIsHttpSdTarget(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1", "__meta_x", `"quoted"<html>`)})

	fragments := a.Snapshot().TargetFragments("collector-1", "job")
	require.Len(t, fragments, 1)
	assert.Equal(t, []decodedTarget{{
		Targets: []string{"10.0.0.1:8080"},
		Labels: map[string]string{
			"__address__":                     "10.0.0.1:8080",
			"__meta_kubernetes_pod_node_name": "node-1",
			"__meta_x":                        `"quoted"<html>`,
		},
	}}, decode(t, fragments))
}

func TestOldestCollectorOnNodeWins(t *testing.T) {
	for _, tc := range []struct {
		name       string
		collectors []*collectors.Collector
		want       string
	}{
		{
			name: "oldest by creation timestamp",
			collectors: []*collectors.Collector{
				newCollector("collector-a", "node-1", t0.Add(time.Minute)),
				newCollector("collector-b", "node-1", t0),
			},
			want: "collector-b",
		},
		{
			name: "same creation timestamp: first by name",
			collectors: []*collectors.Collector{
				newCollector("collector-b", "node-1", t0),
				newCollector("collector-a", "node-1", t0),
			},
			want: "collector-a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range [][]int{{0, 1}, {1, 0}} {
				a := newTestAllocator()
				a.SetCollectors([]*collectors.Collector{tc.collectors[order[0]], tc.collectors[order[1]]})
				a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1")})
				assert.Equal(t, map[string]map[string]int{tc.want: {"job": 1}}, a.Snapshot().Assignments().Collectors)
			}
		})
	}
}

func TestRecomputeOnCollectorChange(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1")})
	before := a.Snapshot()

	a.SetCollectors([]*collectors.Collector{newCollector("collector-2", "node-1", t0)})
	after := a.Snapshot()

	assert.Len(t, before.TargetFragments("collector-1", "job"), 1, "an existing snapshot does not change")
	assert.Empty(t, after.TargetFragments("collector-1", "job"))
	assert.Len(t, after.TargetFragments("collector-2", "job"), 1)
}

func TestFragmentCache(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	target := newTarget("job", "10.0.0.1:8080", "node-1", "__meta_version", "1")
	a.SetTargets([]*targets.Target{target})
	first := a.Snapshot().TargetFragments("collector-1", "job")[0]

	a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1", "__meta_version", "1")})
	second := a.Snapshot().TargetFragments("collector-1", "job")[0]
	assert.Same(t, &first[0], &second[0], "an unchanged target reuses its encoding")

	a.SetTargets([]*targets.Target{newTarget("job", "10.0.0.1:8080", "node-1", "__meta_version", "2")})
	decoded := decode(t, a.Snapshot().TargetFragments("collector-1", "job"))
	assert.Equal(t, "2", decoded[0].Labels["__meta_version"], "a changed meta label results in a new encoding")
	assert.Len(t, a.fragments, 1, "fragments of removed targets are dropped")

	a.SetTargets(nil)
	assert.Empty(t, a.fragments)
}

func TestSameLabelsInDifferentJobsHaveSeparateFragments(t *testing.T) {
	a := newTestAllocator()
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	a.SetTargets([]*targets.Target{
		newTarget("job-a", "10.0.0.1:8080", "node-1"),
		newTarget("job-b", "10.0.0.1:8080", "node-1"),
	})
	assert.Len(t, a.Snapshot().TargetFragments("collector-1", "job-a"), 1)
	assert.Len(t, a.Snapshot().TargetFragments("collector-1", "job-b"), 1)
	assert.Len(t, a.fragments, 2)
}

func TestMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	a := New(discardLogger, meterProvider.Meter(MeterName), []string{PoolPerNode})
	a.SetCollectors([]*collectors.Collector{newCollector("collector-1", "node-1", t0)})
	a.SetTargets([]*targets.Target{
		newTarget("job", "10.0.0.1:8080", "node-1"),
		newTarget("job", "10.0.0.2:8080", "node-2"),
		newTarget("job", "10.0.0.3:8080", ""),
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	values := map[string]int64{}
	var durationRecorded bool
	for _, m := range rm.ScopeMetrics[0].Metrics {
		switch data := m.Data.(type) {
		case metricdata.Gauge[int64]:
			for _, dp := range data.DataPoints {
				values[m.Name+attributeSuffix(dp.Attributes)] = dp.Value
			}
		case metricdata.Histogram[float64]:
			durationRecorded = m.Name == MeterName+".recompute.duration" && len(data.DataPoints) == 1
		}
	}
	assert.Equal(t, map[string]int64{
		MeterName + ".targets":                                         3,
		MeterName + ".targets.assigned{pool=per-node}":                 1,
		MeterName + ".targets.unassigned{reason=no_collector_on_node}": 1,
		MeterName + ".targets.unassigned{reason=no_pool}":              1,
		MeterName + ".collectors{pool=per-node}":                       1,
	}, values)
	assert.True(t, durationRecorded)
}

func attributeSuffix(set attribute.Set) string {
	if set.Len() == 0 {
		return ""
	}
	kv := set.ToSlice()[0]
	return fmt.Sprintf("{%s=%s}", kv.Key[len(MeterName)+1:], kv.Value.AsString())
}

func benchmarkInput(targetCount int, nodeCount int) ([]*targets.Target, []*collectors.Collector) {
	cs := make([]*collectors.Collector, 0, nodeCount)
	for i := range nodeCount {
		cs = append(cs, newCollector(fmt.Sprintf("collector-%d", i), fmt.Sprintf("node-%d", i), t0))
	}
	ts := make([]*targets.Target, 0, targetCount)
	for i := range targetCount {
		ts = append(ts, newTarget(
			fmt.Sprintf("job-%d", i%20),
			fmt.Sprintf("10.%d.%d.%d:8080", i/65536, (i/256)%256, i%256),
			fmt.Sprintf("node-%d", i%nodeCount),
			"__meta_kubernetes_namespace", "namespace",
			"__meta_kubernetes_pod_name", fmt.Sprintf("pod-%d", i),
			"__meta_kubernetes_pod_label_app", "app",
		))
	}
	return ts, cs
}

// BenchmarkRecompute measures a recompute of 100k targets on 1k nodes, either with all targets new (cold) or with all
// fragments in the cache (warm).
func BenchmarkRecompute(b *testing.B) {
	ts, cs := benchmarkInput(100_000, 1_000)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			a := newTestAllocator()
			a.SetCollectors(cs)
			a.SetTargets(ts)
		}
	})
	b.Run("warm", func(b *testing.B) {
		a := newTestAllocator()
		a.SetCollectors(cs)
		a.SetTargets(ts)
		b.ReportAllocs()
		for b.Loop() {
			a.SetTargets(ts)
		}
	})
}
