// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type Agent0ConnectorManager struct {
	client.Client
	agent0ConnectorResourceManager *a0cresources.Agent0ConnectorResourceManager
	enabled                        bool
	developmentMode                bool
	updateInProgress               atomic.Bool
}

type Agent0ConnectorReconcileTrigger string

const (
	TriggeredByWatchEvent                                  Agent0ConnectorReconcileTrigger = "watch"
	TriggeredByDash0OperatorConfigurationResourceReconcile Agent0ConnectorReconcileTrigger = "resource"
)

func NewAgent0ConnectorManager(
	k8sClient client.Client,
	enabled bool,
	developmentMode bool,
	agent0ConnectorResourceManager *a0cresources.Agent0ConnectorResourceManager,
) *Agent0ConnectorManager {
	return &Agent0ConnectorManager{
		Client:                         k8sClient,
		enabled:                        enabled,
		developmentMode:                developmentMode,
		agent0ConnectorResourceManager: agent0ConnectorResourceManager,
	}
}

// agent0ConnectorEnabled reports whether the optional agent0-connector deployment should be managed. It is controlled by
// the Helm value operator.agent0Connector.enabled.
func (m *Agent0ConnectorManager) agent0ConnectorEnabled() bool {
	return m.enabled
}

// ReconcileAgent0Connector can be triggered by
// 1. a reconcile request from the Dash0OperatorConfiguration resource, or
// 2. a change event on one of the agent0-connector related resources that the operator manages.
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due to
// another reconciliation already being in progress. A return value of (true, nil) does not necessarily indicate that
// any agent0-connector resource has been created, updated, or deleted; it only indicates that the reconciliation has
// been performed.
func (m *Agent0ConnectorManager) ReconcileAgent0Connector(
	ctx context.Context,
	trigger Agent0ConnectorReconcileTrigger,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Debug("ReconcileAgent0Connector", "trigger", trigger)

	if m.updateInProgress.Load() {
		if m.developmentMode {
			logger.Info("creation/update of the agent0-connector resources is already in progress, skipping " +
				"additional reconciliation request.")
		}
		return false, nil
	}
	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}

	if operatorConfigurationResource == nil {
		logger.Debug("The Dash0OperatorConfiguration resource is missing or has been deleted, the agent0-connector " +
			"deployment (if present) will be removed.")
		err = m.removeAgent0Connector(ctx, logger)
		return err == nil, err
	}

	if !m.agent0ConnectorEnabled() {
		logger.Debug("The agent0-connector deployment is disabled, it (if present) will be removed.")
		err = m.removeAgent0Connector(ctx, logger)
		return err == nil, err
	}

	err = m.createOrUpdateAgent0Connector(ctx, logger)
	return err == nil, err
}

func (m *Agent0ConnectorManager) createOrUpdateAgent0Connector(
	ctx context.Context,
	logger logd.Logger,
) error {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.agent0ConnectorResourceManager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
	if err != nil {
		logger.Error(err, "failed to create one or more of the agent0-connector resources")
		return err
	}

	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("agent0-connector Kubernetes resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("agent0-connector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("agent0-connector Kubernetes resources have been updated.")
	} else {
		logger.Debug("agent0-connector Kubernetes resources are already up to date, no changes required")
	}

	return nil
}

func (m *Agent0ConnectorManager) removeAgent0Connector(
	ctx context.Context,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.agent0ConnectorResourceManager.DeleteResources(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to delete the agent0-connector Kubernetes resources, requeuing reconcile request.")
		return err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("agent0-connector Kubernetes resources have been deleted.")
	} else {
		logger.Debug("no agent0-connector Kubernetes resources to delete")
	}
	return nil
}
