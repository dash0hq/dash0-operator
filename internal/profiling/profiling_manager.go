// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"context"
	"time"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dash0hq/dash0-operator/internal/profiling/prresources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// Manager decides when the profiler and debuginfo proxy should exist.
type Manager struct {
	ResourceManager *prresources.ResourceManager
}

// NewManager creates a profiling lifecycle manager.
func NewManager(resourceManager *prresources.ResourceManager) *Manager {
	return &Manager{ResourceManager: resourceManager}
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
		configuration.Spec.Profiling != nil && ptr.Deref(configuration.Spec.Profiling.Enabled, true) &&
		configuration.Spec.Profiling.EbpfProfiler != nil && ptr.Deref(configuration.Spec.Profiling.EbpfProfiler.Enabled, false)
	var changes prresources.Changes
	if enabled {
		changes, err = m.ResourceManager.CreateOrUpdateResources(ctx, configuration)
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
