// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/enablement"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type CollectorManager struct {
	client.Client
	nodeMetadataClient          metadata.Interface
	oTelColResourceManager      *otelcolresources.OTelColResourceManager
	extraConfig                 atomic.Pointer[util.ExtraConfig]
	developmentMode             bool
	signalControlFeatureEnabled bool
	enablementChecker           enablement.Checker
	updateInProgress            atomic.Bool
	// now returns the current time. It is overridable in tests to exercise the zone-coverage check interval;
	// NewCollectorManager defaults it to time.Now.
	now func() time.Time
	// lastZoneCoverage remembers the outcome of the most recent availability-zone coverage evaluation, so that the
	// node list backing the check runs at most once per zoneCoverageCheckInterval and the warning is logged on a
	// change of state rather than on every reconcile.
	lastZoneCoverage atomic.Pointer[zoneCoverageState]
}

// zoneCoverageState captures the most recent availability-zone coverage evaluation: when the node list was last
// performed, the zone and replica counts it observed, and whether the warning was active for them.
type zoneCoverageState struct {
	checkedAt    time.Time
	zoneCount    int
	replicaCount int32
	warned       bool
}

// zoneCoverageCheckInterval is the minimum time between two node-list backed availability-zone checks. An explicit
// change to the Signal Control collector replica count bypasses it, so a reconfiguration is reflected without waiting.
const zoneCoverageCheckInterval = 30 * time.Minute

type CollectorReconcileTrigger string

const (
	logMsgOperatorConfigMissing string = "No operator configuration resource exists. No Dash0 OpenTelemetry collector " +
		"will be created, existing Dash0 OpenTelemetry collectors (if any) will be removed."
	logMsgDefaultExportMissing string = "There is an operator configuration resource (\"%s\"), but it has no export " +
		"configuration, no Dash0 OpenTelemetry collector will be created, existing Dash0 OpenTelemetry " +
		"collectors (if any) will be removed."
	logMsgTelemetryDisabled string = "Telemetry collection has been disabled explicitly via the operator configuration " +
		"resource (\"%s\"), property telemetryCollection.enabled=false, no Dash0 OpenTelemetry collector " +
		"will be created, existing Dash0 OpenTelemetry collectors (if any) will be removed."
)

func NewCollectorManager(
	k8sClient client.Client,
	nodeMetadataClient metadata.Interface,
	extraConfig util.ExtraConfig,
	developmentMode bool,
	signalControlFeatureEnabled bool,
	enablementChecker enablement.Checker,
	oTelColResourceManager *otelcolresources.OTelColResourceManager,
) *CollectorManager {
	m := &CollectorManager{
		Client:                      k8sClient,
		nodeMetadataClient:          nodeMetadataClient,
		developmentMode:             developmentMode,
		signalControlFeatureEnabled: signalControlFeatureEnabled,
		enablementChecker:           enablementChecker,
		oTelColResourceManager:      oTelColResourceManager,
	}
	m.now = time.Now
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *CollectorManager) nowOrDefault() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *CollectorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileOpenTelemetryCollector(ctx)
		if err != nil {
			logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update collector resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled collector resources after extra config map update")
		}
	} else {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
	}
}

// ReconcileOpenTelemetryCollector can be triggered by a
//  1. a reconcile request from the Dash0OperatorConfiguration resource.
//  2. a reconcile request from a Dash0Monitoring resource in the cluster.
//  3. a change event on one of the OpenTelemetry collector related resources that the operator manages (a change to one
//     of "our" config maps or similar).
//  4. a file change event picked up by the extra config map watcher
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconciliation already being in progress or because the resource has been deleted by the operator.
// A return value of (nil, true) does not necessarily indicate that any collector resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
func (m *CollectorManager) ReconcileOpenTelemetryCollector(
	ctx context.Context,
) (bool, error) {
	logger := logd.FromContext(ctx)
	if m.updateInProgress.Load() {
		logger.Debug("creation/update of the OpenTelemetry collector resources is already in progress, skipping " +
			"additional reconciliation request.")
		return false, nil
	}

	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	operatorConfigurationResource, err := m.findOperatorConfigurationResource(ctx, logger)
	if err != nil {
		return false, err
	}
	if operatorConfigurationResource != nil {
		logger.Debug("found operator configuration resource for collector reconciliation", "name", operatorConfigurationResource.Name)
	} else {
		logger.Debug("no operator configuration resource found for collector reconciliation")
	}
	allMonitoringResources, err := m.findAllMonitoringResources(ctx, logger)
	if err != nil {
		return false, err
	}
	logger.Debug("found available monitoring resources for collector reconciliation", "count", len(allMonitoringResources))
	var signalControlResource *dash0v1alpha1.Dash0SignalControl
	signalControlEnabled := false
	if m.signalControlFeatureEnabled {
		signalControlResource, err = m.findSignalControlResource(ctx, logger)
		if err != nil {
			return false, err
		}
		if signalControlResource != nil {
			logger.Debug("found Signal Control resource for collector reconciliation", "name", signalControlResource.Name)
		} else {
			logger.Debug("no Signal Control resource found for collector reconciliation")
		}

		signalControlEnabled = signalControlResource != nil &&
			(signalControlResource.Spec.Enabled == nil || *signalControlResource.Spec.Enabled)

		// Gate Signal Control on a Dash0 export in the operator configuration. Signal Control requires a Dash0 export
		// with an auth token for the Decision Maker connection; without it, treat Signal Control as absent so the
		// collector is rendered without any Signal Control components (plain collector image and config). Note the
		// operator configuration export precondition below only removes the collector when there is no export at all;
		// an Http/Grpc-only export would still build a collector, so this Dash0-specific gate is required separately.
		if signalControlEnabled &&
			(operatorConfigurationResource == nil || !operatorConfigurationResource.HasDash0ExportConfigured()) {
			logger.WarnTelemetryCollectionIssue("Signal Control is enabled, but the operator configuration has no " +
				"Dash0 export; Signal Control components will not be added to the collector.")
			signalControlResource = nil
		}

		// Gate Signal Control on the organization's entitlement. If the organization is not entitled (or the
		// entitlement cannot be confirmed), treat Signal Control as absent so the collector is rendered without any
		// Signal Control components (plain collector image and config).
		if signalControlResource != nil &&
			signalControlEnabled &&
			m.enablementChecker != nil &&
			operatorConfigurationResource != nil &&
			!m.enablementChecker.EnsureAllowed(ctx, operatorConfigurationResource, logger) {
			logger.WarnTelemetryCollectionIssue("The organization is not entitled to use Signal Control (or the " +
				"entitlement could not be confirmed); Signal Control components will not be added to the collector.")
			signalControlResource = nil
		}
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in CollectorManager#ReconcileOpenTelemetryCollector")
	}

	if operatorConfigurationResource == nil {
		logger.WarnTelemetryCollectionIssue(logMsgOperatorConfigMissing)
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true) {
		logger.Info(fmt.Sprintf(logMsgTelemetryDisabled, operatorConfigurationResource.Name))
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !operatorConfigurationResource.HasExportsConfigured() {
		logger.Info(fmt.Sprintf(logMsgDefaultExportMissing, operatorConfigurationResource.Name))
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else {
		// Only relevant when a Signal Control collector is actually deployed: the resource may exist while being
		// explicitly disabled, not entitled, or without a Dash0 export, in which case there is nothing to spread over
		// availability zones.
		if signalControlResource != nil && signalControlEnabled {
			m.warnAboutInsufficientZoneCoverage(ctx, *extraConfig, logger)
		}
		err = m.createOrUpdateOpenTelemetryCollector(
			ctx,
			operatorConfigurationResource,
			allMonitoringResources,
			signalControlResource,
			*extraConfig,
			logger,
		)
		return err == nil, err
	}
}

// warnAboutInsufficientZoneCoverage warns when the cluster has more availability zones than the Signal Control
// collector has replicas. The collector's service prefers endpoints in the sender's own zone, but kube-proxy can only
// do that for zones that actually have a ready endpoint; senders in the remaining zones fall back to the full endpoint
// set and their telemetry crosses zones.
//
// This deliberately only warns. Deriving the replica count from the zone count would mean writing spec.replicas from
// the reconciler that also watches that very deployment, so any nondeterminism in the zone count would turn into
// replica churn - and every restarted replica discards its tail-sampling reservoir.
func (m *CollectorManager) warnAboutInsufficientZoneCoverage(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) {
	if m.nodeMetadataClient == nil {
		return
	}

	replicaCount := extraConfig.SignalControlCollectorReplicas
	if replicaCount < 1 {
		replicaCount = otelcolresources.SignalControlCollectorDefaultReplicas
	}

	now := m.nowOrDefault()
	previous := m.lastZoneCoverage.Load()
	replicaCountChanged := previous != nil && previous.replicaCount != replicaCount
	// The node list is the expensive part of the check, so it is performed at most once per zoneCoverageCheckInterval.
	// An explicit change to the replica count bypasses the interval, so a reconfiguration is evaluated - and its
	// warning re-logged, or an info logged when it resolved the issue - without waiting for the next interval.
	if previous != nil && !replicaCountChanged && now.Sub(previous.checkedAt) < zoneCoverageCheckInterval {
		return
	}

	// The metadata client bypasses the controller-runtime cache on purpose: reading nodes through the cached client
	// would start an informer that keeps every node object in memory for the lifetime of the operator. Only object
	// metadata is requested, since the zone label is all that is read, which keeps the node status (in particular the
	// image list) off the wire. The read is served from the API server's watch cache (ResourceVersion "0") and
	// restricted to nodes that carry a zone label, since nodes without one contribute nothing to the zone count.
	nodes, err := m.nodeMetadataClient.
		Resource(corev1.SchemeGroupVersion.WithResource("nodes")).
		List(ctx, metav1.ListOptions{
			ResourceVersion: "0",
			LabelSelector:   corev1.LabelTopologyZone,
		})
	if err != nil {
		logger.Debug("cannot list nodes to check the Signal Control collector's availability zone coverage", "error", err)
		return
	}
	zones := make(map[string]struct{})
	for _, node := range nodes.Items {
		if zone := node.Labels[corev1.LabelTopologyZone]; zone != "" {
			zones[zone] = struct{}{}
		}
	}

	m.reportZoneCoverage(len(zones), replicaCount, now, logger)
}

// reportZoneCoverage evaluates the availability-zone coverage for the given zone and replica counts, logs the warning
// when there are more zones than replicas, and records the outcome under checkedAt. In a steady state the warning is
// logged once, on the transition into it, not on every check. A replica-count change is always acted on, so the
// warning is re-logged if the situation persists. When a previously warned situation resolves - by more replicas or a
// lower zone count - a short info is logged once.
func (m *CollectorManager) reportZoneCoverage(
	zoneCount int,
	replicaCount int32,
	checkedAt time.Time,
	logger logd.Logger,
) {
	previous := m.lastZoneCoverage.Load()
	wasWarned := previous != nil && previous.warned

	// With zero or one zone there is nothing to spread over, the zone preference is inert either way.
	insufficient := zoneCount > 1 && int32(zoneCount) > replicaCount

	m.lastZoneCoverage.Store(&zoneCoverageState{
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
		logger.WarnTelemetryCollectionIssue(fmt.Sprintf(
			"The cluster has %d availability zones but the Signal Control collector runs with %d replicas, so at least "+
				"one zone has no Signal Control collector pod. Telemetry from those zones is sent to a collector in "+
				"another zone, which works but incurs cross-zone traffic cost. Set "+
				"operator.collectors.signalControlCollectorReplicas to at least %d to avoid that.",
			zoneCount, replicaCount, zoneCount,
		))
		return
	}

	// Resolved: announce it whenever a previously active warning has cleared, whether the replica count was raised
	// or the zone count dropped on its own.
	if wasWarned {
		logger.Info(fmt.Sprintf(
			"The Signal Control collector now runs with %d replicas across %d availability zones, so every zone has a "+
				"Signal Control collector pod and cross-zone traffic is avoided.",
			replicaCount, zoneCount,
		))
	}
}

func (m *CollectorManager) createOrUpdateOpenTelemetryCollector(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	slices.SortFunc(
		allMonitoringResources,
		func(mr1 dash0v1beta1.Dash0Monitoring, mr2 dash0v1beta1.Dash0Monitoring) int {
			return strings.Compare(mr1.Namespace, mr2.Namespace)
		},
	)
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			extraConfig,
			operatorConfigurationResource,
			allMonitoringResources,
			signalControlResource,
			logger,
		)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(
			err,
			"failed to create one or more of the OpenTelemetry collector DaemonSet/Deployment resources, some or "+
				"all telemetry will be missing",
		)
		return err
	}
	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been updated.")
	} else {
		logger.Debug("OpenTelemetry collector Kubernetes resources are already up to date, no changes required")
	}
	return nil
}

func (m *CollectorManager) removeOpenTelemetryCollector(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.oTelColResourceManager.DeleteResources(
		ctx,
		extraConfig,
		logger,
	)
	if err != nil {
		logger.Error(
			err,
			"Failed to delete the OpenTelemetry collector Kubernetes resources, requeuing reconcile request.",
		)
		return err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("OpenTelemetry collector Kubernetes resources have been deleted.")
	} else {
		logger.Debug("no OpenTelemetry collector Kubernetes resources to delete")
	}
	return nil
}

func (m *CollectorManager) findOperatorConfigurationResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	operatorConfigurationResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"", /* cluster-scope, thus no namespace */
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if operatorConfigurationResource == nil {
		return nil, nil
	}
	return operatorConfigurationResource.(*dash0v1alpha1.Dash0OperatorConfiguration), nil
}

func (m *CollectorManager) findSignalControlResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0SignalControl, error) {
	signalControlResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"",
		&dash0v1alpha1.Dash0SignalControl{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if signalControlResource == nil {
		return nil, nil
	}
	return signalControlResource.(*dash0v1alpha1.Dash0SignalControl), nil
}

func (m *CollectorManager) findAllMonitoringResources(
	ctx context.Context,
	logger logd.Logger,
) ([]dash0v1beta1.Dash0Monitoring, error) {
	monitoringResourceList := dash0v1beta1.Dash0MonitoringList{}
	if err := m.List(
		ctx,
		&monitoringResourceList,
		&client.ListOptions{},
	); err != nil {
		logger.ErrorTelemetryCollectionIssue(err, "Failed to list all Dash0 monitoring resources, requeuing reconcile request.")
		return nil, err
	}

	// filter monitoring resources that are not in state available
	monitoringResources := make([]dash0v1beta1.Dash0Monitoring, 0, len(monitoringResourceList.Items))
	for _, mr := range monitoringResourceList.Items {
		availableCondition := meta.FindStatusCondition(
			mr.Status.Conditions,
			string(dash0common.ConditionTypeAvailable),
		)
		if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
			continue
		}
		monitoringResources = append(monitoringResources, mr)
	}
	logger.Debug(
		"filtered monitoring resources by availability",
		"total", len(monitoringResourceList.Items),
		"available", len(monitoringResources),
	)
	return monitoringResources, nil
}
