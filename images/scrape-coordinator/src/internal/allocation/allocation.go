// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package allocation assigns the scrape targets to collectors. Every change of the targets or the collectors results in
// a full recompute into an immutable Snapshot, which is published atomically and read without locks.
package allocation

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/collectors"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/targets"
)

const (
	// PoolPerNode is the pool of collectors that scrape the targets on their own node.
	PoolPerNode = "per-node"

	// ReasonNoCollectorOnNode means that the per-node pool has no eligible collector on the target's node.
	ReasonNoCollectorOnNode = "no_collector_on_node"
	// ReasonNoPool means that no configured pool is responsible for the target, e.g. because its node is unknown.
	ReasonNoPool = "no_pool"

	// MeterName is the name of the scrape coordinator's meter, and the prefix of the name of every metric it reports.
	MeterName = "dash0.operator.scrape_coordinator"

	poolAttributeKey   = attribute.Key(MeterName + ".pool")
	reasonAttributeKey = attribute.Key(MeterName + ".reason")
)

// picker selects the collector of a pool for a target.
type picker interface {
	// pick returns the name of the collector that scrapes the target, or false if the pool has none for it.
	pick(target *targets.Target) (string, bool)
	// unassignedReason is the reason reported for a target for which pick returns false.
	unassignedReason() string
}

// route returns the pool that is responsible for the target, or false if no configured pool is.
func route(target *targets.Target, pickers map[string]picker) (string, bool) {
	if target.NodeName != "" {
		if _, ok := pickers[PoolPerNode]; ok {
			return PoolPerNode, true
		}
	}
	return "", false
}

// perNodePicker assigns a target to the collector on the target's node.
type perNodePicker struct {
	byNode map[string]*collectors.Collector
}

// newPerNodePicker creates a perNodePicker. If a node has several collectors, e.g. during a rolling update with surge,
// the oldest one wins, so the assignment does not depend on the order of the collectors.
func newPerNodePicker(poolCollectors []*collectors.Collector) *perNodePicker {
	byNode := make(map[string]*collectors.Collector, len(poolCollectors))
	for _, c := range poolCollectors {
		if existing, ok := byNode[c.NodeName]; !ok || isOlder(c, existing) {
			byNode[c.NodeName] = c
		}
	}
	return &perNodePicker{byNode: byNode}
}

func isOlder(a, b *collectors.Collector) bool {
	if !a.CreationTimestamp.Equal(b.CreationTimestamp) {
		return a.CreationTimestamp.Before(b.CreationTimestamp)
	}
	return a.Name < b.Name
}

func (p *perNodePicker) pick(target *targets.Target) (string, bool) {
	c, ok := p.byNode[target.NodeName]
	if !ok {
		return "", false
	}
	return c.Name, true
}

func (*perNodePicker) unassignedReason() string {
	return ReasonNoCollectorOnNode
}

// fragmentKey identifies the encoded form of a target. The hash covers all discovered labels including the meta labels,
// so that a change of a meta label results in a new fragment.
type fragmentKey struct {
	job        string
	labelsHash uint64
}

// targetJSON is a target in the Prometheus HTTP service discovery format.
type targetJSON struct {
	Targets []string      `json:"targets"`
	Labels  labels.Labels `json:"labels"`
}

// Snapshot is an immutable assignment of targets to collectors.
type Snapshot struct {
	// byCollector maps collector name -> job name -> encoded targets. The encoded targets are shared between snapshots
	// and must not be modified.
	byCollector     map[string]map[string][][]byte
	targets         int
	assignedPerPool map[string]int
	unassigned      map[string]int
}

// TargetFragments returns the encoded targets of the job that are assigned to the collector, each one a JSON object in
// the Prometheus HTTP service discovery format. The result must not be modified.
func (s *Snapshot) TargetFragments(collector string, job string) [][]byte {
	return s.byCollector[collector][job]
}

// Assignments is a summary of a snapshot.
type Assignments struct {
	// Targets is the number of distinct targets.
	Targets int `json:"targets"`
	// Pools maps pool name -> number of assigned targets.
	Pools map[string]int `json:"pools"`
	// Collectors maps collector name -> job name -> number of targets.
	Collectors map[string]map[string]int `json:"collectors"`
	// Unassigned maps reason -> number of targets.
	Unassigned map[string]int `json:"unassigned"`
}

// Assignments returns a summary of the snapshot.
func (s *Snapshot) Assignments() Assignments {
	result := Assignments{
		Targets:    s.targets,
		Pools:      maps.Clone(s.assignedPerPool),
		Collectors: make(map[string]map[string]int, len(s.byCollector)),
		Unassigned: maps.Clone(s.unassigned),
	}
	for collector, jobs := range s.byCollector {
		counts := make(map[string]int, len(jobs))
		for job, fragments := range jobs {
			counts[job] = len(fragments)
		}
		result.Collectors[collector] = counts
	}
	return result
}

// Allocator computes a new Snapshot whenever the targets or the collectors change.
type Allocator struct {
	logger *slog.Logger
	pools  []string

	mu                 sync.Mutex // Guards the fields below, serializes the recomputes.
	targets            []*targets.Target
	collectors         []*collectors.Collector
	targetsReceived    bool
	collectorsReceived bool
	fragments          map[fragmentKey][]byte
	lastUnassigned     map[string]int

	snapshot atomic.Pointer[Snapshot]

	targetsMetric           otelmetric.Int64Gauge
	assignedTargetsMetric   otelmetric.Int64Gauge
	unassignedTargetsMetric otelmetric.Int64Gauge
	collectorsMetric        otelmetric.Int64Gauge
	recomputeDurationMetric otelmetric.Float64Histogram
}

// New creates an Allocator for the given pools. An instrument that cannot be created is reported as an error log
// message and left unset, so that its metric is not reported.
func New(logger *slog.Logger, meter otelmetric.Meter, pools []string) *Allocator {
	a := &Allocator{
		logger:         logger,
		pools:          pools,
		fragments:      map[fragmentKey][]byte{},
		lastUnassigned: map[string]int{},
	}
	a.targetsMetric = newGauge(logger, meter, "targets", "Number of distinct targets to be scraped")
	a.assignedTargetsMetric = newGauge(logger, meter, "targets.assigned", "Number of targets assigned per pool")
	a.unassignedTargetsMetric = newGauge(logger, meter, "targets.unassigned", "Number of unassigned targets per reason")
	a.collectorsMetric = newGauge(logger, meter, "collectors", "Number of eligible collectors per pool")
	recomputeDurationMetricName := MeterName + ".recompute.duration"
	if histogram, err := meter.Float64Histogram(
		recomputeDurationMetricName,
		otelmetric.WithUnit("s"),
		otelmetric.WithDescription("Duration of recomputing the assignment of targets to collectors"),
	); err != nil {
		logger.Error("cannot initialize the metric", "metric.name", recomputeDurationMetricName, "error", err)
	} else {
		a.recomputeDurationMetric = histogram
	}
	return a
}

func newGauge(logger *slog.Logger, meter otelmetric.Meter, name string, description string) otelmetric.Int64Gauge {
	metricName := MeterName + "." + name
	gauge, err := meter.Int64Gauge(metricName, otelmetric.WithUnit("1"), otelmetric.WithDescription(description))
	if err != nil {
		logger.Error("cannot initialize the metric", "metric.name", metricName, "error", err)
		return nil
	}
	return gauge
}

// Snapshot returns the current assignment, or nil as long as the targets or the collectors have not been received yet.
func (a *Allocator) Snapshot() *Snapshot {
	return a.snapshot.Load()
}

// SetTargets replaces the targets and recomputes the assignment.
func (a *Allocator) SetTargets(targets []*targets.Target) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.targets = targets
	a.targetsReceived = true
	a.recompute()
}

// SetCollectors replaces the eligible collectors of all pools and recomputes the assignment.
func (a *Allocator) SetCollectors(eligibleCollectors []*collectors.Collector) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.collectors = eligibleCollectors
	a.collectorsReceived = true
	a.recompute()
}

func (a *Allocator) recompute() {
	// Without the collectors, every target would be reported as unassigned. Without the targets, the collectors would
	// drop all their targets.
	if !a.targetsReceived || !a.collectorsReceived {
		return
	}
	start := time.Now()

	collectorsPerPool := make(map[string][]*collectors.Collector, len(a.pools))
	for _, c := range a.collectors {
		collectorsPerPool[c.Pool] = append(collectorsPerPool[c.Pool], c)
	}
	pickers := make(map[string]picker, len(a.pools))
	for _, pool := range a.pools {
		if pool == PoolPerNode {
			pickers[pool] = newPerNodePicker(collectorsPerPool[pool])
		}
	}

	debugEnabled := a.logger.Enabled(context.Background(), slog.LevelDebug)
	fragments := make(map[fragmentKey][]byte, len(a.targets))
	byCollector := map[string]map[string][][]byte{}
	assignedPerPool := map[string]int{}
	unassigned := map[string]int{}
	for _, t := range a.targets {
		collector, pool, reason := a.assign(t, pickers)
		if collector == "" {
			unassigned[reason]++
			if debugEnabled {
				a.logger.Debug("the target is not assigned to a collector",
					"job", t.Job, "address", t.Address, "node", t.NodeName, "reason", reason)
			}
			continue
		}
		assignedPerPool[pool]++
		fragment := a.fragment(t, fragments)
		if fragment == nil {
			continue
		}
		jobs, ok := byCollector[collector]
		if !ok {
			jobs = map[string][][]byte{}
			byCollector[collector] = jobs
		}
		jobs[t.Job] = append(jobs[t.Job], fragment)
	}
	a.fragments = fragments

	a.snapshot.Store(&Snapshot{
		byCollector:     byCollector,
		targets:         len(a.targets),
		assignedPerPool: assignedPerPool,
		unassigned:      unassigned,
	})

	a.logUnassignedChanges(unassigned)
	a.recordMetrics(len(a.targets), assignedPerPool, unassigned, collectorsPerPool, time.Since(start))
}

// assign returns the collector and pool of the target, or the reason why it is not assigned.
func (a *Allocator) assign(t *targets.Target, pickers map[string]picker) (string, string, string) {
	pool, ok := route(t, pickers)
	if !ok {
		return "", "", ReasonNoPool
	}
	p := pickers[pool]
	collector, ok := p.pick(t)
	if !ok {
		return "", pool, p.unassignedReason()
	}
	return collector, pool, ""
}

// fragment returns the encoded target, reusing the encoding of the previous recompute if it exists.
func (a *Allocator) fragment(t *targets.Target, fragments map[fragmentKey][]byte) []byte {
	key := fragmentKey{job: t.Job, labelsHash: t.Labels.Hash()}
	if fragment, ok := fragments[key]; ok {
		return fragment
	}
	fragment, ok := a.fragments[key]
	if !ok {
		var err error
		fragment, err = json.Marshal(targetJSON{Targets: []string{t.Address}, Labels: t.Labels})
		if err != nil {
			a.logger.Error("cannot encode the target", "job", t.Job, "address", t.Address, "error", err)
			return nil
		}
	}
	fragments[key] = fragment
	return fragment
}

func (a *Allocator) logUnassignedChanges(unassigned map[string]int) {
	for reason, count := range unassigned {
		if a.lastUnassigned[reason] != count {
			a.logger.Info("the number of unassigned targets has changed", "reason", reason, "count", count)
		}
	}
	for reason := range a.lastUnassigned {
		if _, ok := unassigned[reason]; !ok {
			a.logger.Info("the number of unassigned targets has changed", "reason", reason, "count", 0)
		}
	}
	a.lastUnassigned = unassigned
}

func (a *Allocator) recordMetrics(
	targets int,
	assignedPerPool map[string]int,
	unassigned map[string]int,
	collectorsPerPool map[string][]*collectors.Collector,
	duration time.Duration,
) {
	ctx := context.Background()
	if a.targetsMetric != nil {
		a.targetsMetric.Record(ctx, int64(targets))
	}
	for _, pool := range a.pools {
		poolAttribute := otelmetric.WithAttributes(poolAttributeKey.String(pool))
		if a.assignedTargetsMetric != nil {
			a.assignedTargetsMetric.Record(ctx, int64(assignedPerPool[pool]), poolAttribute)
		}
		if a.collectorsMetric != nil {
			a.collectorsMetric.Record(ctx, int64(len(collectorsPerPool[pool])), poolAttribute)
		}
	}
	if a.unassignedTargetsMetric != nil {
		for _, reason := range []string{ReasonNoCollectorOnNode, ReasonNoPool} {
			a.unassignedTargetsMetric.Record(
				ctx, int64(unassigned[reason]), otelmetric.WithAttributes(reasonAttributeKey.String(reason)))
		}
	}
	if a.recomputeDurationMetric != nil {
		a.recomputeDurationMetric.Record(ctx, duration.Seconds())
	}
}
