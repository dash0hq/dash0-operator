// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/metadata"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// ZoneCoverageCheckInterval is the minimum time between two node-list backed availability-zone checks. An explicit
// change to the observed replica count bypasses it, so a reconfiguration is reflected without waiting.
const ZoneCoverageCheckInterval = 30 * time.Minute

// zoneCoverageState captures the most recent availability-zone coverage evaluation: when the node list was last
// performed, the zone and replica counts it observed, and whether the warning was active for them.
type zoneCoverageState struct {
	checkedAt    time.Time
	zoneCount    int
	replicaCount int32
	warned       bool
}

// ZoneCoverageMessages provides the component-specific wording for a ZoneCoverageReporter. Warn is logged as a
// telemetry-collection issue when there are more availability zones than replicas; Resolved is logged as info, once,
// when a previously active warning clears; ListErrDebug is logged at debug level when the node list fails.
type ZoneCoverageMessages struct {
	Warn         func(zoneCount int, replicaCount int32) string
	Resolved     func(zoneCount int, replicaCount int32) string
	ListErrDebug string
}

// ZoneCoverageReporter warns when a zone-preferring workload has fewer replicas than the cluster has availability
// zones. Its service prefers endpoints in the sender's own zone, but kube-proxy can only do that for zones that have a
// ready endpoint; senders in the remaining zones fall back to the full endpoint set and their traffic crosses zones.
// The node list backing the check is the expensive part, so it runs at most once per ZoneCoverageCheckInterval and the
// warning is logged on a change of state rather than on every call.
type ZoneCoverageReporter struct {
	nodeMetadataClient metadata.Interface
	// now returns the current time. It is overridable in tests to exercise the check interval; NewZoneCoverageReporter
	// defaults it to time.Now.
	now  func() time.Time
	last atomic.Pointer[zoneCoverageState]
}

// ZoneCoverageReporterOption customizes a ZoneCoverageReporter.
type ZoneCoverageReporterOption func(*ZoneCoverageReporter)

// WithClock overrides the reporter's time source. Intended for tests exercising the check interval.
func WithClock(now func() time.Time) ZoneCoverageReporterOption {
	return func(r *ZoneCoverageReporter) {
		r.now = now
	}
}

// NewZoneCoverageReporter returns a reporter backed by the given node metadata client. A nil client turns Report into a
// no-op, mirroring clusters where the operator has no node read access.
func NewZoneCoverageReporter(nodeMetadataClient metadata.Interface, opts ...ZoneCoverageReporterOption) *ZoneCoverageReporter {
	r := &ZoneCoverageReporter{
		nodeMetadataClient: nodeMetadataClient,
		now:                time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Report lists the cluster's zone-labelled nodes - at most once per ZoneCoverageCheckInterval, unless replicaCount
// changed since the last call - and logs msgs.Warn when there are more availability zones than replicaCount, or
// msgs.Resolved once when a previous warning clears. It is a no-op when the reporter has no node metadata client.
func (r *ZoneCoverageReporter) Report(
	ctx context.Context,
	replicaCount int32,
	msgs ZoneCoverageMessages,
	logger logd.Logger,
) {
	if r == nil || r.nodeMetadataClient == nil {
		return
	}

	now := r.nowOrDefault()
	previous := r.last.Load()
	replicaCountChanged := previous != nil && previous.replicaCount != replicaCount
	// The node list is the expensive part of the check, so it is performed at most once per ZoneCoverageCheckInterval.
	// An explicit change to the replica count bypasses the interval, so a reconfiguration is evaluated - and its
	// warning re-logged, or an info logged when it resolved the issue - without waiting for the next interval.
	if previous != nil && !replicaCountChanged && now.Sub(previous.checkedAt) < ZoneCoverageCheckInterval {
		return
	}

	// The metadata client bypasses the controller-runtime cache on purpose: reading nodes through the cached client
	// would start an informer that keeps every node object in memory for the lifetime of the operator. Only object
	// metadata is requested, since the zone label is all that is read, which keeps the node status (in particular the
	// image list) off the wire. The read is served from the API server's watch cache (ResourceVersion "0") and
	// restricted to nodes that carry a zone label, since nodes without one contribute nothing to the zone count.
	nodes, err := r.nodeMetadataClient.
		Resource(corev1.SchemeGroupVersion.WithResource("nodes")).
		List(ctx, metav1.ListOptions{
			ResourceVersion: "0",
			LabelSelector:   corev1.LabelTopologyZone,
		})
	if err != nil {
		logger.Debug(msgs.ListErrDebug, "error", err)
		return
	}
	zones := make(map[string]struct{})
	for _, node := range nodes.Items {
		if zone := node.Labels[corev1.LabelTopologyZone]; zone != "" {
			zones[zone] = struct{}{}
		}
	}

	r.report(len(zones), replicaCount, now, msgs, logger)
}

func (r *ZoneCoverageReporter) nowOrDefault() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// report evaluates the availability-zone coverage for the given zone and replica counts, logs the warning when there
// are more zones than replicas, and records the outcome under checkedAt. In a steady state the warning is logged once,
// on the transition into it, not on every check. A replica-count change is always acted on, so the warning is
// re-logged if the situation persists. When a previously warned situation resolves - by more replicas or a lower zone
// count - a short info is logged once.
func (r *ZoneCoverageReporter) report(
	zoneCount int,
	replicaCount int32,
	checkedAt time.Time,
	msgs ZoneCoverageMessages,
	logger logd.Logger,
) {
	previous := r.last.Load()
	wasWarned := previous != nil && previous.warned

	// With zero or one zone there is nothing to spread over, the zone preference is inert either way.
	insufficient := zoneCount > 1 && int32(zoneCount) > replicaCount

	r.last.Store(&zoneCoverageState{
		checkedAt:    checkedAt,
		zoneCount:    zoneCount,
		replicaCount: replicaCount,
		warned:       insufficient,
	})

	if insufficient {
		// A steady state - the same zone and replica count already warned about - is not warned about again.
		if wasWarned && previous.zoneCount == zoneCount && previous.replicaCount == replicaCount {
			return
		}
		logger.WarnTelemetryCollectionIssue(msgs.Warn(zoneCount, replicaCount))
		return
	}

	// Resolved: announce it whenever a previously active warning has cleared, whether the replica count was raised or
	// the zone count dropped on its own.
	if wasWarned {
		logger.Info(msgs.Resolved(zoneCount, replicaCount))
	}
}
