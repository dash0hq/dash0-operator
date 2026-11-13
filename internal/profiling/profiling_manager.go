// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/dash0hq/dash0-operator/internal/profiling/prresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// Manager decides when the profiler and debuginfo proxy should exist.
type Manager struct {
	ResourceManager     *prresources.ResourceManager
	extraConfig         atomic.Pointer[util.ExtraConfig]
	extraConfigDefaults util.ExtraConfig
	// extraConfigUpdates requests a reconciliation from the profiling controller. It holds at most one pending request,
	// since a reconciliation always reads the latest extra config.
	extraConfigUpdates chan event.TypedGenericEvent[struct{}]
}

// NewManager creates a profiling lifecycle manager.
func NewManager(
	resourceManager *prresources.ResourceManager,
	extraConfig util.ExtraConfig,
	extraConfigDefaults util.ExtraConfig,
) *Manager {
	m := &Manager{
		ResourceManager:     resourceManager,
		extraConfigDefaults: extraConfigDefaults,
		extraConfigUpdates:  make(chan event.TypedGenericEvent[struct{}], 1),
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

// UpdateExtraConfig applies an updated extra config map (e.g. changed resources, labels or tolerations) to the profiler
// and debuginfo proxy resources. The reconciliation runs in the profiling controller, i.e. only on the leader.
func (m *Manager) UpdateExtraConfig(_ context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig != nil && reflect.DeepEqual(*previousConfig, newConfig) {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
		return
	}
	select {
	case m.extraConfigUpdates <- event.TypedGenericEvent[struct{}]{}:
		logger.Info("requested a reconciliation of the profiler and debuginfo proxy resources after extra config map update")
	default:
		logger.Debug("a reconciliation of the profiler and debuginfo proxy resources is already pending")
	}
}

// Reconcile applies the current configuration or removes disabled profiling resources.
func (m *Manager) Reconcile(ctx context.Context) (ctrl.Result, error) {
	logger := logd.FromContext(ctx)
	configuration, err := resources.FindOperatorConfigurationResource(ctx, m.ResourceManager.Client, logger)
	if err != nil {
		return ctrl.Result{}, err
	}
	config := m.ResourceManager.Config
	enabled := config.Enabled && config.TelemetryCollectionEnabled && configuration != nil &&
		ptr.Deref(configuration.Spec.TelemetryCollection.Enabled, true) && len(configuration.EffectiveExports()) > 0 &&
		configuration.Spec.Profiling != nil && ptr.Deref(configuration.Spec.Profiling.Enabled, false) &&
		configuration.Spec.Profiling.EbpfProfiler != nil && ptr.Deref(configuration.Spec.Profiling.EbpfProfiler.Enabled, false)
	var changes prresources.Changes
	if enabled {
		fileExtraConfig := m.extraConfig.Load()
		if fileExtraConfig == nil {
			return ctrl.Result{}, fmt.Errorf("extra config is nil in the profiling manager")
		}
		settings := util.EffectiveExtraConfig(*fileExtraConfig, m.extraConfigDefaults, configuration, nil)
		changes, err = m.ResourceManager.CreateOrUpdateResources(ctx, configuration, settings)
	} else {
		changes, err = m.ResourceManager.DeleteResources(ctx, false)
	}
	if err != nil {
		logger.Error(err, "Failed to reconcile profiler and debuginfo proxy Kubernetes resources")
		return ctrl.Result{}, err
	}
	if changes.Created && changes.Updated {
		logger.Info("Profiler and debuginfo proxy Kubernetes resources have been created and updated")
	} else if changes.Created {
		logger.Info("Profiler and debuginfo proxy Kubernetes resources have been created")
	} else if changes.Updated {
		logger.Info("Profiler and debuginfo proxy Kubernetes resources have been updated")
	} else if !changes.Deleted {
		logger.Debug("Profiler and debuginfo proxy Kubernetes resources require no changes")
	}
	if changes.Deleted {
		logger.Info("Profiler or debuginfo proxy Kubernetes resources have been deleted")
	}
	if changes.PendingDeletion {
		logger.Debug("Waiting for debuginfo proxy pods to terminate before deleting proxy dependencies")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, nil
}
