// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package intelligentedge

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/intelligentedge/ieresources"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type IntelligentEdgeManager struct {
	client.Client
	resourceManager  *ieresources.IntelligentEdgeResourceManager
	extraConfig      atomic.Pointer[util.ExtraConfig]
	updateInProgress atomic.Bool
}

func NewIntelligentEdgeManager(
	k8sClient client.Client,
	resourceManager *ieresources.IntelligentEdgeResourceManager,
	extraConfig util.ExtraConfig,
) *IntelligentEdgeManager {
	m := &IntelligentEdgeManager{
		Client:          k8sClient,
		resourceManager: resourceManager,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *IntelligentEdgeManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.Reconcile(ctx)
		if err != nil {
			logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update intelligent edge resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled intelligent edge resources after extra config map update")
		}
	} else {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
	}
}

// Reconcile looks up the Dash0IntelligentEdge singleton and reconciles it. Intended for callers that react to changes
// in resources other than the IE resource itself (e.g. OperatorConfigurationReconciler on self-monitoring toggles). If
// no IE resource exists, this is a no-op. The IE controller uses ReconcileIntelligentEdge directly with the resource
// that triggered its reconcile request.
func (m *IntelligentEdgeManager) Reconcile(ctx context.Context) (bool, error) {
	logger := logd.FromContext(ctx)
	intelligentEdgeResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"",
		&dash0v1alpha1.Dash0IntelligentEdge{},
		logger,
	)
	if err != nil {
		return false, err
	}
	if intelligentEdgeResource == nil {
		return false, nil
	}
	return m.ReconcileIntelligentEdge(ctx, intelligentEdgeResource.(*dash0v1alpha1.Dash0IntelligentEdge))
}

func (m *IntelligentEdgeManager) ReconcileIntelligentEdge(
	ctx context.Context,
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Info("Reconciling intelligent edge.")

	if !m.updateInProgress.CompareAndSwap(false, true) {
		logger.Info("Reconciliation of intelligent edge resources is already in progress, skipping.")
		return false, nil
	}
	defer func() {
		m.updateInProgress.Store(false)
	}()

	if intelligentEdgeResource == nil {
		logger.Info("The intelligent edge resource has been deleted, removing intelligent edge components.")
		return m.removeIntelligentEdge(ctx)
	}

	if intelligentEdgeResource.Spec.Enabled != nil && !*intelligentEdgeResource.Spec.Enabled {
		logger.Info("Intelligent edge is disabled, removing intelligent edge components.")
		return m.removeIntelligentEdge(ctx)
	}

	logger.Info("Intelligent edge is enabled, reconciling intelligent edge components.")
	return m.createOrUpdateIntelligentEdge(ctx, intelligentEdgeResource)
}

func (m *IntelligentEdgeManager) createOrUpdateIntelligentEdge(
	ctx context.Context,
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
) (bool, error) {
	logger := logd.FromContext(ctx)

	operatorConfig, err := m.findOperatorConfigurationResource(ctx)
	if err != nil {
		logger.Error(err, "failed to find operator configuration resource")
		return false, err
	}
	if operatorConfig == nil {
		logger.Info("No operator configuration resource found. Intelligent edge components will be created " +
			"with incomplete configuration (missing endpoints and authorization). Create an operator " +
			"configuration resource with a Dash0 export to complete the setup.")
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in IntelligentEdgeManager#createOrUpdateIntelligentEdge")
	}

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.resourceManager.CreateOrUpdateResources(ctx, intelligentEdgeResource, operatorConfig, *extraConfig, logger)
	if err != nil {
		logger.Error(err, "failed to create/update intelligent edge resources")
		return false, err
	}
	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("Intelligent edge resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("Intelligent edge resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("Intelligent edge resources have been updated.")
	}
	return true, nil
}

func (m *IntelligentEdgeManager) removeIntelligentEdge(ctx context.Context) (bool, error) {
	logger := logd.FromContext(ctx)
	resourcesHaveBeenDeleted, err := m.resourceManager.DeleteResources(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to delete intelligent edge resources.")
		return false, err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("intelligent edge resources have been deleted")
	}
	return true, nil
}

func (m *IntelligentEdgeManager) findOperatorConfigurationResource(
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
