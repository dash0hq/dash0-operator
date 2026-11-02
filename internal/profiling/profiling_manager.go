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
	configuration, err := resources.FindOperatorConfigurationResource(ctx, m.ResourceManager.Client, logd.FromContext(ctx))
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
		changes, err = m.ResourceManager.CreateOrUpdateResources(ctx, configuration)
	} else {
		changes, err = m.ResourceManager.DeleteResources(ctx, false)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if changes.PendingDeletion {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, nil
}
