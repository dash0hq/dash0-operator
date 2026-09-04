// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	k8sretry "k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type Agent0ConnectorManager struct {
	client.Client
	agent0ConnectorResourceManager *a0cresources.Agent0ConnectorResourceManager
	eventRecorder                  events.EventRecorder
	extraConfig                    atomic.Pointer[util.ExtraConfig]
	enabledViaHelm                 bool
	developmentMode                bool
	reconcileGuard                 util.ReconcileGuard
}

type Agent0ConnectorReconcileTrigger string

const (
	TriggeredByWatchEvent                                  Agent0ConnectorReconcileTrigger = "watch"
	TriggeredByDash0OperatorConfigurationResourceReconcile Agent0ConnectorReconcileTrigger = "resource"
)

func NewAgent0ConnectorManager(
	k8sClient client.Client,
	enabledViaHelm bool,
	extraConfig util.ExtraConfig,
	developmentMode bool,
	agent0ConnectorResourceManager *a0cresources.Agent0ConnectorResourceManager,
	eventRecorder events.EventRecorder,
) *Agent0ConnectorManager {
	m := &Agent0ConnectorManager{
		Client:                         k8sClient,
		enabledViaHelm:                 enabledViaHelm,
		developmentMode:                developmentMode,
		agent0ConnectorResourceManager: agent0ConnectorResourceManager,
		eventRecorder:                  eventRecorder,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

// UpdateExtraConfig applies an updated extra config map (e.g. changed labels, annotations, tolerations or node
// affinity) to the agent0-connector resources managed by the operator.
func (m *Agent0ConnectorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)
		if err != nil {
			logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update agent0-connector resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled agent0-connector resources after extra config map update")
		}
	} else {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
	}
}

// ReconcileAgent0Connector can be triggered by
// 1. a reconcile request from the Dash0OperatorConfiguration resource, or
// 2. a change event on one of the agent0-connector related resources that the operator manages.
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due to
// another reconciliation already being in progress or due to the agent0-connector being misconfigured. A return value
// of (true, nil) does not necessarily indicate that any agent0-connector resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
//
// A request that arrives while a reconciliation is in progress is not executed. The reconciliation which is in progress
// repeats itself once it is done, see util.ReconcileGuard. Without that, a corrected extra config map arriving
// mid-reconciliation would be stored but never applied, since neither controller requeues periodically.
//
// A misconfiguration of the agent0-connector is reported as (false, nil): the error is not passed on to the caller,
// since requeuing the reconcile request cannot fix it, and since reconciling agent0-connector must not block the
// caller's remaining reconciliation steps. It is reported in the status of the Dash0OperatorConfiguration resource
// (status.agent0Connector) and, on a change of the outcome, as a Kubernetes event.
func (m *Agent0ConnectorManager) ReconcileAgent0Connector(
	ctx context.Context,
	trigger Agent0ConnectorReconcileTrigger,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Debug("ReconcileAgent0Connector", "trigger", trigger)

	return m.reconcileGuard.Run(
		func() (bool, error) {
			return m.reconcileAgent0Connector(ctx, logger)
		},
		func() {
			if m.developmentMode {
				logger.Info("creation/update of the agent0-connector resources is already in progress, the " +
					"additional reconciliation request will be served by the reconciliation which is in progress.")
			}
		},
	)
}

// reconcileAgent0Connector is the body of ReconcileAgent0Connector, executed under the manager's reconcile guard. It
// reads the operator configuration resource and the extra config itself, which is what allows the guard to repeat it
// for a trigger that arrived while it was running.
func (m *Agent0ConnectorManager) reconcileAgent0Connector(ctx context.Context, logger logd.Logger) (bool, error) {
	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in Agent0ConnectorManager#ReconcileAgent0Connector")
	}

	if operatorConfigurationResource == nil {
		logger.Debug("The Dash0OperatorConfiguration resource is missing or has been deleted, the agent0-connector " +
			"deployment (if present) will be removed.")
		err = m.removeAgent0Connector(ctx, *extraConfig, logger)
		return err == nil, err
	}

	if !m.agent0ConnectorEnabled(operatorConfigurationResource) {
		logger.Debug("The agent0-connector deployment is disabled, it (if present) will be removed.")
		if err = m.removeAgent0Connector(ctx, *extraConfig, logger); err != nil {
			return false, err
		}
		m.clearAgent0ConnectorStatus(ctx, operatorConfigurationResource, logger)
		return true, nil
	}

	hasBeenReconciled, err := m.createOrUpdateAgent0Connector(ctx, *extraConfig, logger)
	m.reportAgent0ConnectorStatus(ctx, operatorConfigurationResource, err, logger)
	if errors.Is(err, a0cresources.ErrMisconfigured) {
		// Requeuing the reconcile request cannot fix a Helm-level misconfiguration, and agent0-connector deployment must
		// not block the remaining reconciliation steps for the Dash0OperatorConfiguration resource. The status and the
		// Kubernetes event above report it instead.
		return false, nil
	}
	return hasBeenReconciled, err
}

// agent0ConnectorEnabled reports whether the optional agent0-connector deployment should be managed. It requires the
// Helm value operator.agent0Connector.enabled, which users can override with spec.agent0Connector.enabled of the
// Dash0OperatorConfiguration resource to opt out.
func (m *Agent0ConnectorManager) agent0ConnectorEnabled(
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
) bool {
	return operatorConfigurationResource.Spec.Agent0Connector.IsEnabled(m.enabledViaHelm)
}

// reportAgent0ConnectorStatus records the outcome of the last attempt to create or update the agent0-connector
// resources in the status of the Dash0OperatorConfiguration resource and queues a Kubernetes event when the outcome
// changed.
func (m *Agent0ConnectorManager) reportAgent0ConnectorStatus(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	reconcileErr error,
	logger logd.Logger,
) {
	deployed := reconcileErr == nil
	reason := StatusReasonDeployed
	message := "The operator has deployed the agent0-connector."
	if !deployed {
		reason = agent0ConnectorFailureReason(reconcileErr)
		message = agent0ConnectorFailureMessage(reason, reconcileErr)
	}

	changed, err := m.updateAgent0ConnectorStatus(
		ctx,
		operatorConfigurationResource,
		func(resource *dash0v1alpha1.Dash0OperatorConfiguration) bool {
			return resource.SetAgent0ConnectorStatus(deployed, reason, message)
		},
	)
	if err != nil {
		logger.Error(err, "cannot record the agent0-connector status in the Dash0OperatorConfiguration resource")
		return
	}
	if !changed {
		return
	}
	if deployed {
		util.QueueAgent0ConnectorDeployedEvent(m.eventRecorder, operatorConfigurationResource)
	} else {
		util.QueueAgent0ConnectorNotDeployedEvent(m.eventRecorder, operatorConfigurationResource, message)
	}
}

// clearAgent0ConnectorStatus removes the agent0-connector status, so that a disabled agent0-connector does not leave a
// stale entry behind.
func (m *Agent0ConnectorManager) clearAgent0ConnectorStatus(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) {
	if _, err := m.updateAgent0ConnectorStatus(
		ctx,
		operatorConfigurationResource,
		func(resource *dash0v1alpha1.Dash0OperatorConfiguration) bool {
			return resource.RemoveAgent0ConnectorStatus()
		},
	); err != nil {
		logger.Error(err, "cannot remove the agent0-connector status from the Dash0OperatorConfiguration resource")
	}
}

// updateAgent0ConnectorStatus applies the given modification to the status of the Dash0OperatorConfiguration resource
// and reports whether it changed anything. The resource is read again for every attempt: the agent0-connector is
// reconciled from three independent triggers, which can collide with each other and with the status update of the
// operator configuration controller.
func (m *Agent0ConnectorManager) updateAgent0ConnectorStatus(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	modify func(*dash0v1alpha1.Dash0OperatorConfiguration) bool,
) (bool, error) {
	changed := false
	err := k8sretry.RetryOnConflict(k8sretry.DefaultRetry, func() error {
		resource := &dash0v1alpha1.Dash0OperatorConfiguration{}
		if err := m.Get(ctx, client.ObjectKeyFromObject(operatorConfigurationResource), resource); err != nil {
			return err
		}
		changed = modify(resource)
		if !changed {
			return nil
		}
		return m.Status().Update(ctx, resource)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// The programmatic identifiers reported in status.agent0Connector.reason of the Dash0OperatorConfiguration resource.
// They name the individual outcome and are therefore more specific than the reason of the Kubernetes event, which only
// distinguishes deployed from not deployed. A new failure mode needs an entry here and in
// agent0ConnectorFailureReason, otherwise it is reported as StatusReasonReconcileFailed.
const (
	StatusReasonDeployed                   = "Deployed"
	StatusReasonInvalidClusterRoleRules    = "InvalidClusterRoleRules"
	StatusReasonNoAuthorizationToken       = "NoAuthorizationToken"
	StatusReasonOperatorMissingPermissions = "OperatorMissingPermissions"
	StatusReasonReconcileFailed            = "ReconcileFailed"
)

// agent0ConnectorFailureReason maps a reconcile error to the programmatic identifier reported in the status.
func agent0ConnectorFailureReason(err error) string {
	switch {
	case errors.Is(err, a0cresources.ErrInvalidClusterRoleRules):
		return StatusReasonInvalidClusterRoleRules
	case errors.Is(err, a0cresources.ErrNoAuthorizationToken):
		return StatusReasonNoAuthorizationToken
	case apierrors.IsForbidden(err):
		return StatusReasonOperatorMissingPermissions
	default:
		return StatusReasonReconcileFailed
	}
}

// agent0ConnectorFailureMessage describes a reconcile failure for the status and for the Kubernetes event.
func agent0ConnectorFailureMessage(reason string, err error) string {
	if reason != StatusReasonOperatorMissingPermissions {
		return err.Error()
	}
	return fmt.Sprintf(
		"The API server rejected an agent0-connector resource as forbidden. One possible cause is Kubernetes' "+
			"privilege escalation prevention (the operator can only grant permissions it holds itself). The API "+
			"server reported: %s",
		err.Error(),
	)
}

// createOrUpdateAgent0Connector creates or updates the agent0-connector resources. The returned flag reports whether
// the resources have been reconciled. Every error is passed on, including a misconfiguration, which the caller reports
// via the status and a Kubernetes event before it stops the error from reaching its own caller.
func (m *Agent0ConnectorManager) createOrUpdateAgent0Connector(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) (bool, error) {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.agent0ConnectorResourceManager.CreateOrUpdateAgent0ConnectorResources(ctx, extraConfig, logger)
	if err != nil {
		if !errors.Is(err, a0cresources.ErrMisconfigured) {
			// The resource manager has already logged the details of a misconfiguration.
			logger.Error(err, "failed to create one or more of the agent0-connector resources")
		}
		return false, err
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

	return true, nil
}

func (m *Agent0ConnectorManager) removeAgent0Connector(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.agent0ConnectorResourceManager.DeleteResources(ctx, extraConfig, logger)
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
