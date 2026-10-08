// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"k8s.io/client-go/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/scresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type SignalControlManager struct {
	client.Client
	resourceManager  *scresources.SignalControlResourceManager
	extraConfig      atomic.Pointer[util.ExtraConfig]
	updateInProgress atomic.Bool
	// zoneCoverageReporter warns when the Edge Proxy has fewer replicas than the cluster has availability zones. See
	// cluster.ZoneCoverageReporter.
	zoneCoverageReporter *cluster.ZoneCoverageReporter
}

func NewSignalControlManager(
	k8sClient client.Client,
	resourceManager *scresources.SignalControlResourceManager,
	nodeMetadataClient metadata.Interface,
	extraConfig util.ExtraConfig,
) *SignalControlManager {
	m := &SignalControlManager{
		Client:               k8sClient,
		resourceManager:      resourceManager,
		zoneCoverageReporter: cluster.NewZoneCoverageReporter(nodeMetadataClient),
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *SignalControlManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.Reconcile(ctx)
		if err != nil {
			logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update Signal Control resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled Signal Control resources after extra config map update")
		}
	} else {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
	}
}

// Reconcile looks up the Dash0SignalControl singleton and reconciles it. Intended for callers that react to changes
// in resources other than the Signal Control resource itself (e.g. OperatorConfigurationReconciler on self-monitoring toggles). If
// no Signal Control resource exists, this is a no-op. The Signal Control controller uses ReconcileSignalControl directly with the resource
// that triggered its reconcile request.
func (m *SignalControlManager) Reconcile(ctx context.Context) (bool, error) {
	logger := logd.FromContext(ctx)
	signalControlResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"",
		&dash0v1alpha1.Dash0SignalControl{},
		logger,
	)
	if err != nil {
		return false, err
	}
	if signalControlResource == nil {
		return false, nil
	}
	return m.ReconcileSignalControl(ctx, signalControlResource.(*dash0v1alpha1.Dash0SignalControl))
}

func (m *SignalControlManager) ReconcileSignalControl(
	ctx context.Context,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Info("Reconciling Signal Control.")

	if !m.updateInProgress.CompareAndSwap(false, true) {
		logger.Info("Reconciliation of Signal Control resources is already in progress, skipping.")
		return false, nil
	}
	defer func() {
		m.updateInProgress.Store(false)
	}()

	if signalControlResource == nil {
		logger.Info("The Signal Control resource has been deleted, removing Signal Control components.")
		return m.removeSignalControl(ctx)
	}

	if signalControlResource.Spec.Enabled != nil && !*signalControlResource.Spec.Enabled {
		logger.Info("Signal Control is disabled, removing Signal Control components.")
		return m.removeSignalControl(ctx)
	}

	// Gate the Signal Control components (in particular the Edge Proxy) on a Dash0 export in the operator
	// configuration. Signal Control requires a Dash0 export with an auth token for the Decision Maker connection;
	// without it, callers that react to unrelated changes (e.g. the extra config map watcher via Reconcile) must not
	// deploy the Edge Proxy. The Signal Control controller marks the resource degraded in the same situation.
	operatorConfig, err := m.findOperatorConfigurationResource(ctx)
	if err != nil {
		logger.Error(err, "failed to find operator configuration resource")
		return false, err
	}
	if operatorConfig == nil || !operatorConfig.HasDash0ExportConfigured() {
		logger.Info("Signal Control is enabled, but the operator configuration has no Dash0 export; removing Signal " +
			"Control components.")
		return m.removeSignalControl(ctx)
	}

	logger.Info("Signal Control is enabled, reconciling Signal Control components.")
	return m.createOrUpdateSignalControl(ctx, signalControlResource, operatorConfig)
}

func (m *SignalControlManager) createOrUpdateSignalControl(
	ctx context.Context,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
) (bool, error) {
	logger := logd.FromContext(ctx)

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in SignalControlManager#createOrUpdateSignalControl")
	}

	// Only relevant when the Edge Proxy is actually deployed: with it disabled there is nothing to spread over
	// availability zones.
	if pointers.ReadBoolPointerWithDefault(signalControlResource.Spec.EdgeProxy.Enabled, true) {
		m.warnAboutInsufficientZoneCoverage(ctx, *extraConfig, logger)
	}

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.resourceManager.CreateOrUpdateResources(ctx, signalControlResource, operatorConfig, *extraConfig, logger)
	if err != nil {
		logger.Error(err, "failed to create/update Signal Control resources")
		return false, err
	}
	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("Signal Control resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("Signal Control resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("Signal Control resources have been updated.")
	}
	return true, nil
}

// warnAboutInsufficientZoneCoverage warns when the cluster has more availability zones than the Edge Proxy has
// replicas. The Edge Proxy service prefers endpoints in the sender's own zone, but kube-proxy can only do that for
// zones that actually have a ready endpoint; collectors in the remaining zones fall back to the full endpoint set and
// their decision stream to the Edge Proxy crosses zones.
//
// Like the Signal Control collector's check, this deliberately only warns and never derives the replica count from the
// zone count: writing spec.replicas from the reconciler that also watches that deployment would turn any
// nondeterminism in the zone count into replica churn.
func (m *SignalControlManager) warnAboutInsufficientZoneCoverage(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) {
	replicaCount := extraConfig.EdgeProxyReplicas
	if replicaCount < 1 {
		replicaCount = scresources.EdgeProxyDefaultReplicas
	}
	m.zoneCoverageReporter.Report(ctx, replicaCount, cluster.ZoneCoverageMessages{
		Warn: func(zoneCount int, replicaCount int32) string {
			return fmt.Sprintf(
				"The cluster has %d availability zones but the Edge Proxy runs with %d replicas, so at least one zone "+
					"has no Edge Proxy pod. Collectors in those zones connect to an Edge Proxy in another zone, which "+
					"works but incurs cross-zone traffic cost. Set operator.signalControl.edgeProxy.replicas to at "+
					"least %d to avoid that.",
				zoneCount, replicaCount, zoneCount,
			)
		},
		Resolved: func(zoneCount int, replicaCount int32) string {
			return fmt.Sprintf(
				"The Edge Proxy now runs with %d replicas across %d availability zones, so every zone has an Edge Proxy "+
					"pod and cross-zone traffic is avoided.",
				replicaCount, zoneCount,
			)
		},
		ListErrDebug: "cannot list nodes to check the Edge Proxy's availability zone coverage",
	}, logger)
}

func (m *SignalControlManager) removeSignalControl(ctx context.Context) (bool, error) {
	logger := logd.FromContext(ctx)
	resourcesHaveBeenDeleted, err := m.resourceManager.DeleteResources(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to delete Signal Control resources.")
		return false, err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("Signal Control resources have been deleted")
	}
	return true, nil
}

func (m *SignalControlManager) findOperatorConfigurationResource(
	ctx context.Context,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	logger := logd.FromContext(ctx)
	resource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"",
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, nil
	}
	return resource.(*dash0v1alpha1.Dash0OperatorConfiguration), nil
}
