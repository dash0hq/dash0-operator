// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package targets turns the target groups of Prometheus' service discovery into the targets the collectors scrape.
// Whether a target is scraped is decided by Prometheus' own scrape logic, the same that the Prometheus receiver of the
// collectors runs, so both agree on which targets survive relabeling.
package targets

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/scrape"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/debounce"
)

// DefaultUpdateInterval is the minimum time between two target updates. Service discovery reports changes in bursts.
const DefaultUpdateInterval = 5 * time.Second

// defaultDiscoveryTimeout is the maximum time to wait for the first report of service discovery.
const defaultDiscoveryTimeout = 30 * time.Second

// nodeNameLabels identify the node a target runs on, in order of precedence.
var nodeNameLabels = []string{
	"__meta_kubernetes_pod_node_name",
	"__meta_kubernetes_node_name",
	"__meta_kubernetes_endpoint_node_name",
	"__meta_kubernetes_endpointslice_endpoint_node_name",
}

const (
	endpointSliceAddressTargetKindLabel = "__meta_kubernetes_endpointslice_address_target_kind"
	endpointSliceAddressTargetNameLabel = "__meta_kubernetes_endpointslice_address_target_name"
)

// Target is a target to be scraped by one collector.
//
// Targets are shared between snapshots, their fields must not be modified after creation.
type Target struct {
	// Job is the name of the scrape config the target belongs to.
	Job string
	// Address is the value of the __address__ label before relabeling.
	Address string
	// Labels are the discovered labels before relabeling, including the __meta_* labels, because the collectors relabel
	// the target again.
	Labels labels.Labels
	// NodeName is the node the target runs on, or empty if it is not known.
	NodeName string
}

// NewTarget creates a target from its discovered labels.
func NewTarget(job string, discoveredLabels labels.Labels) *Target {
	return &Target{
		Job:      job,
		Address:  discoveredLabels.Get(model.AddressLabel),
		Labels:   discoveredLabels,
		NodeName: nodeName(discoveredLabels),
	}
}

func nodeName(discoveredLabels labels.Labels) string {
	for _, name := range nodeNameLabels {
		if value := discoveredLabels.Get(name); value != "" {
			return value
		}
	}
	if discoveredLabels.Get(endpointSliceAddressTargetKindLabel) == "Node" {
		return discoveredLabels.Get(endpointSliceAddressTargetNameLabel)
	}
	return ""
}

// Builder keeps the latest scrape configs and target groups, and builds the targets from them.
type Builder struct {
	logger           *slog.Logger
	trigger          *debounce.Trigger
	discoveryTimeout time.Duration

	// The maps are replaced as a whole and never modified, so they can be read after releasing the lock.
	mu            sync.Mutex
	scrapeConfigs map[string]*promconfig.ScrapeConfig
	groups        map[string][]*targetgroup.Group
	// stopWaitingForDiscovery is set once service discovery has reported, or once the discovery timeout has expired
	// since waitingForDiscoverySince, the time the first jobs were set.
	stopWaitingForDiscovery  bool
	waitingForDiscoverySince time.Time
}

// NewBuilder creates a Builder that publishes the targets at most once per update interval.
func NewBuilder(logger *slog.Logger, updateInterval time.Duration) *Builder {
	return &Builder{
		logger:           logger,
		trigger:          debounce.New(updateInterval),
		discoveryTimeout: defaultDiscoveryTimeout,
		scrapeConfigs:    map[string]*promconfig.ScrapeConfig{},
		groups:           map[string][]*targetgroup.Group{},
	}
}

// SetScrapeConfigs replaces the scrape configs. Their relabel configs decide which discovered targets are kept, and
// target groups of jobs without a scrape config are ignored.
func (b *Builder) SetScrapeConfigs(scrapeConfigs []*promconfig.ScrapeConfig) {
	byJob := make(map[string]*promconfig.ScrapeConfig, len(scrapeConfigs))
	for _, scrapeConfig := range scrapeConfigs {
		byJob[scrapeConfig.JobName] = scrapeConfig
	}
	b.mu.Lock()
	b.scrapeConfigs = byJob
	if len(byJob) > 0 && b.waitingForDiscoverySince.IsZero() {
		b.waitingForDiscoverySince = time.Now()
	}
	b.mu.Unlock()
	b.trigger.Notify()
}

// Run receives the target groups from service discovery and publishes the resulting targets after every change, at
// most once per update interval, until ctx is cancelled. Each update of the service discovery contains the groups of
// all jobs. The first targets are published once service discovery has reported, once there are scrape configs without
// any job, because service discovery does not report anything then, or once the discovery timeout has expired.
func (b *Builder) Run(ctx context.Context, updates <-chan map[string][]*targetgroup.Group, publish func([]*Target)) {
	go b.trigger.Run(ctx, func() {
		if b.mayPublish() {
			publish(b.Build())
		}
	})
	discoveryTimeout := time.NewTimer(b.discoveryTimeout)
	defer discoveryTimeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-discoveryTimeout.C:
			if remaining := b.stopWaitingForDiscoveryAfterTimeout(); remaining > 0 {
				discoveryTimeout.Reset(remaining)
			}
		case groups := <-updates:
			b.mu.Lock()
			b.groups = groups
			b.stopWaitingForDiscovery = true
			b.mu.Unlock()
			b.trigger.Notify()
		}
	}
}

// stopWaitingForDiscoveryAfterTimeout stops waiting for service discovery once the discovery timeout has expired since
// the first jobs were set, and otherwise returns the remaining time.
func (b *Builder) stopWaitingForDiscoveryAfterTimeout() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopWaitingForDiscovery {
		return 0
	}
	if b.waitingForDiscoverySince.IsZero() {
		return b.discoveryTimeout
	}
	if remaining := b.discoveryTimeout - time.Since(b.waitingForDiscoverySince); remaining > 0 {
		return remaining
	}
	b.stopWaitingForDiscovery = true
	b.logger.Info("service discovery has not reported yet, publishing the targets without waiting any longer",
		"timeout", b.discoveryTimeout.String())
	b.trigger.Notify()
	return 0
}

func (b *Builder) mayPublish() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopWaitingForDiscovery || len(b.scrapeConfigs) == 0
}

// Build returns the targets of all jobs that are kept by the relabel configs of their scrape config.
func (b *Builder) Build() []*Target {
	b.mu.Lock()
	scrapeConfigs, groups := b.scrapeConfigs, b.groups
	b.mu.Unlock()

	var result []*Target
	for job, jobGroups := range groups {
		scrapeConfig, ok := scrapeConfigs[job]
		if !ok {
			// The scrape config has been removed and service discovery has not caught up yet.
			continue
		}
		result = append(result, b.buildJob(scrapeConfig, jobGroups)...)
	}
	return result
}

// buildJob returns the targets of one job. Targets that are identical after relabeling are scraped once, so only one
// of them is kept: the one with the smallest discovered label set, which does not depend on the order of the groups.
func (b *Builder) buildJob(scrapeConfig *promconfig.ScrapeConfig, groups []*targetgroup.Group) []*Target {
	relabelBuilder := labels.NewBuilder(labels.EmptyLabels())
	discoveredBuilder := labels.NewScratchBuilder(0)
	byRelabeledLabels := map[uint64]*Target{}
	var order []uint64
	for _, group := range groups {
		for _, targetLabels := range group.Targets {
			relabeled, err := scrape.PopulateLabels(relabelBuilder, scrapeConfig, targetLabels, group.Labels)
			if err != nil {
				b.logger.Debug("skipping an invalid target", "job", scrapeConfig.JobName,
					"address", string(targetLabels[model.AddressLabel]), "error", err)
				continue
			}
			if relabeled.IsEmpty() {
				continue
			}
			target := NewTarget(scrapeConfig.JobName,
				discoveredLabels(&discoveredBuilder, targetLabels, group.Labels))
			key := relabeled.Hash()
			existing, ok := byRelabeledLabels[key]
			if !ok {
				order = append(order, key)
			}
			if !ok || labels.Compare(target.Labels, existing.Labels) < 0 {
				byRelabeledLabels[key] = target
			}
		}
	}
	result := make([]*Target, 0, len(order))
	for _, key := range order {
		result = append(result, byRelabeledLabels[key])
	}
	return result
}

// discoveredLabels merges the labels of a target with the labels of its group. A target label takes precedence over a
// group label with the same name, as in Prometheus' scrape logic.
func discoveredLabels(
	builder *labels.ScratchBuilder,
	targetLabels model.LabelSet,
	groupLabels model.LabelSet,
) labels.Labels {
	builder.Reset()
	for name, value := range targetLabels {
		builder.Add(string(name), string(value))
	}
	for name, value := range groupLabels {
		if _, ok := targetLabels[name]; !ok {
			builder.Add(string(name), string(value))
		}
	}
	builder.Sort()
	return builder.Labels()
}
