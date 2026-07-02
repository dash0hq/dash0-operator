// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/scresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type SignalControlManager struct {
	client.Client
	resourceManager  *scresources.SignalControlResourceManager
	extraConfig      atomic.Pointer[util.ExtraConfig]
	updateInProgress atomic.Bool
}

func NewSignalControlManager(
	k8sClient client.Client,
	resourceManager *scresources.SignalControlResourceManager,
	extraConfig util.ExtraConfig,
) *SignalControlManager {
	m := &SignalControlManager{
		Client:          k8sClient,
		resourceManager: resourceManager,
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

	logger.Info("Signal Control is enabled, reconciling Signal Control components.")
	return m.createOrUpdateSignalControl(ctx, signalControlResource)
}

func (m *SignalControlManager) createOrUpdateSignalControl(
	ctx context.Context,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
) (bool, error) {
	logger := logd.FromContext(ctx)

	operatorConfig, err := m.findOperatorConfigurationResource(ctx)
	if err != nil {
		logger.Error(err, "failed to find operator configuration resource")
		return false, err
	}
	if operatorConfig == nil {
		logger.Info("No operator configuration resource found. Signal Control components will be created " +
			"with incomplete configuration (missing endpoints and authorization). Create an operator " +
			"configuration resource with a Dash0 export to complete the setup.")
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in SignalControlManager#createOrUpdateSignalControl")
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
