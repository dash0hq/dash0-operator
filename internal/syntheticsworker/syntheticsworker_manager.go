// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package syntheticsworker

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	k8sretry "k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type SyntheticsWorkerManager struct {
	client.Client
	syntheticsWorkerResourceManager *swresources.SyntheticsWorkerResourceManager
	eventRecorder                   events.EventRecorder
	developmentMode                 bool
	reconcileGuard                  util.ReconcileGuard
}

type SyntheticsWorkerReconcileTrigger string

const (
	TriggeredByWatchEvent                                  SyntheticsWorkerReconcileTrigger = "watch"
	TriggeredByDash0OperatorConfigurationResourceReconcile SyntheticsWorkerReconcileTrigger = "resource"

	syntheticsWorkerDisabledMessage = "The operator has not deployed the synthetics-worker because it is disabled " +
		"in the Dash0 operator configuration resource (syntheticsWorker.enabled: false)."
)

func NewSyntheticsWorkerManager(
	k8sClient client.Client,
	developmentMode bool,
	syntheticsWorkerResourceManager *swresources.SyntheticsWorkerResourceManager,
	eventRecorder events.EventRecorder,
) *SyntheticsWorkerManager {
	return &SyntheticsWorkerManager{
		Client:                          k8sClient,
		developmentMode:                 developmentMode,
		syntheticsWorkerResourceManager: syntheticsWorkerResourceManager,
		eventRecorder:                   eventRecorder,
	}
}

// ReconcileSyntheticsWorker can be triggered by
// 1. a reconcile request from the Dash0OperatorConfiguration resource, or
// 2. a change event on one of the synthetics-worker related resources that the operator manages.
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconciliation already being in progress or due to the synthetics-worker being misconfigured. A return
// value of (true, nil) does not necessarily indicate that any synthetics-worker resource has been created, updated,
// or deleted; it only indicates that the reconciliation has been performed.
//
// A request that arrives while a reconciliation is in progress is not executed. The reconciliation which is in
// progress repeats itself once it is done, see util.ReconcileGuard.
//
// A misconfiguration of the synthetics-worker is reported as (false, nil): the error is not passed on to the caller,
// since requeuing the reconcile request cannot fix it (it depends on a user editing the resource), and since
// reconciling the synthetics-worker must not block the caller's remaining reconciliation steps. It is reported in the
// status of the Dash0OperatorConfiguration resource (status.syntheticsWorker) and, on a change of the outcome, as a
// Kubernetes event.
func (m *SyntheticsWorkerManager) ReconcileSyntheticsWorker(
	ctx context.Context,
	trigger SyntheticsWorkerReconcileTrigger,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Debug("ReconcileSyntheticsWorker", "trigger", trigger)

	return m.reconcileGuard.Run(
		func() (bool, error) {
			return m.reconcileSyntheticsWorker(ctx, logger)
		},
		func() {
			if m.developmentMode {
				logger.Info("creation/update of the synthetics-worker resources is already in progress, the " +
					"additional reconciliation request will be served by the reconciliation which is in progress.")
			}
		},
	)
}

// reconcileSyntheticsWorker is the body of ReconcileSyntheticsWorker, executed under the manager's reconcile guard.
func (m *SyntheticsWorkerManager) reconcileSyntheticsWorker(ctx context.Context, logger logd.Logger) (bool, error) {
	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}

	if operatorConfigurationResource == nil {
		logger.Debug("The Dash0OperatorConfiguration resource is missing or has been deleted, the synthetics-worker " +
			"deployment (if present) will be removed.")
		err = m.removeSyntheticsWorker(ctx, logger)
		return err == nil, err
	}

	if !m.syntheticsWorkerEnabled(operatorConfigurationResource) {
		logger.Debug("The synthetics-worker deployment is disabled, it (if present) will be removed.")
		if err = m.removeSyntheticsWorker(ctx, logger); err != nil {
			return false, err
		}
		m.reportSyntheticsWorkerDisabled(ctx, operatorConfigurationResource, logger)
		return true, nil
	}

	results, hasBeenReconciled, err := m.createOrUpdateSyntheticsWorker(ctx, operatorConfigurationResource, logger)
	m.reportSyntheticsWorkerStatus(ctx, operatorConfigurationResource, results, logger)
	if isNonBlockingSyntheticsWorkerError(err) {
		return false, nil
	}
	return hasBeenReconciled, err
}

// isNonBlockingSyntheticsWorkerError reports whether a reconcile error must not cause a reconcile retry. Requeuing
// cannot fix a misconfiguration of the CRD resource (it requires a user to edit the resource) or a permission the
// operator itself does not hold (it requires a Helm upgrade), and either must not block the remaining reconciliation
// steps for the Dash0OperatorConfiguration resource. The status and a Kubernetes event report it instead.
func isNonBlockingSyntheticsWorkerError(err error) bool {
	return errors.Is(err, swresources.ErrMisconfigured) || apierrors.IsForbidden(err)
}

// syntheticsWorkerEnabled reports whether the optional synthetics-worker deployment should be managed. The
// SyntheticsWorkerManager only exists when the synthetics-worker is enabled via the Helm chart
// (operator.syntheticsWorker.enabled), see setupSyntheticsWorkerManager, so only the opt-out in
// spec.syntheticsWorker.enabled of the Dash0OperatorConfiguration resource is evaluated here.
func (m *SyntheticsWorkerManager) syntheticsWorkerEnabled(
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
) bool {
	return pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.SyntheticsWorker.Enabled, true)
}

// reportSyntheticsWorkerStatus records the outcome of the last attempt to create or update every synthetics-worker
// instance in the status of the Dash0OperatorConfiguration resource and queues a Kubernetes event when the aggregate
// outcome changed. A per-instance failure is reflected in the corresponding InstanceResult; the caller has already
// logged a feature-wide reconcileErr, if any.
func (m *SyntheticsWorkerManager) reportSyntheticsWorkerStatus(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	results []swresources.InstanceResult,
	logger logd.Logger,
) {
	instanceStatuses := make([]dash0v1alpha1.SyntheticsWorkerInstanceStatus, 0, len(results))
	for _, result := range results {
		deployed := result.Err == nil
		reason := StatusReasonDeployed
		message := "The operator has deployed this synthetics-worker instance."
		if !deployed {
			reason = syntheticsWorkerFailureReason(result.Err)
			message = syntheticsWorkerFailureMessage(reason, result.Err)
		}
		instanceStatuses = append(instanceStatuses, dash0v1alpha1.SyntheticsWorkerInstanceStatus{
			LocationID: result.LocationID,
			Deployed:   deployed,
			Reason:     reason,
			Message:    message,
		})
	}

	var aggregateDeployed bool
	var aggregateMessage string
	changed, err := m.updateSyntheticsWorkerStatus(
		ctx,
		operatorConfigurationResource,
		func(resource *dash0v1alpha1.Dash0OperatorConfiguration) bool {
			changed := resource.SetSyntheticsWorkerStatus(instanceStatuses)
			aggregateDeployed = resource.Status.SyntheticsWorker.Deployed
			aggregateMessage = resource.Status.SyntheticsWorker.Message
			return changed
		},
	)
	if err != nil {
		logger.Error(err, "cannot record the synthetics-worker status in the Dash0OperatorConfiguration resource")
		return
	}
	if !changed {
		return
	}
	if aggregateDeployed {
		util.QueueSyntheticsWorkerDeployedEvent(m.eventRecorder, operatorConfigurationResource)
	} else {
		util.QueueSyntheticsWorkerNotDeployedEvent(m.eventRecorder, operatorConfigurationResource, aggregateMessage)
	}
}

// reportSyntheticsWorkerDisabled records in the status of the Dash0OperatorConfiguration resource that the
// synthetics-worker is not deployed because it is disabled, and queues a Kubernetes event when the outcome changed.
func (m *SyntheticsWorkerManager) reportSyntheticsWorkerDisabled(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) {
	changed, err := m.updateSyntheticsWorkerStatus(
		ctx,
		operatorConfigurationResource,
		func(resource *dash0v1alpha1.Dash0OperatorConfiguration) bool {
			return resource.SetSyntheticsWorkerDisabledStatus(StatusReasonDisabled, syntheticsWorkerDisabledMessage)
		},
	)
	if err != nil {
		logger.Error(err, "cannot record the synthetics-worker status in the Dash0OperatorConfiguration resource")
		return
	}
	if !changed {
		return
	}
	util.QueueSyntheticsWorkerDisabledEvent(m.eventRecorder, operatorConfigurationResource, syntheticsWorkerDisabledMessage)
}

// updateSyntheticsWorkerStatus applies the given modification to the status of the Dash0OperatorConfiguration
// resource and reports whether it changed anything. The resource is read again for every attempt: the
// synthetics-worker is reconciled from multiple independent triggers, which can collide with each other and with the
// status update of the operator configuration controller.
func (m *SyntheticsWorkerManager) updateSyntheticsWorkerStatus(
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

// The programmatic identifiers reported in status.syntheticsWorker.reason of the Dash0OperatorConfiguration resource.
// They name the individual outcome and are therefore more specific than the reason of the Kubernetes event, which
// only distinguishes deployed, not deployed and disabled. A new failure mode needs an entry here and in
// syntheticsWorkerFailureReason, otherwise it is reported as StatusReasonReconcileFailed.
const (
	StatusReasonDeployed                   = "Deployed"
	StatusReasonDisabled                   = "Disabled"
	StatusReasonNoAuthorizationToken       = "NoAuthorizationToken"
	StatusReasonOperatorMissingPermissions = "OperatorMissingPermissions"
	StatusReasonReconcileFailed            = "ReconcileFailed"
)

// syntheticsWorkerFailureReason maps a reconcile error to the programmatic identifier reported in the status.
func syntheticsWorkerFailureReason(err error) string {
	switch {
	case errors.Is(err, swresources.ErrNoAuthorizationToken):
		return StatusReasonNoAuthorizationToken
	case apierrors.IsForbidden(err):
		return StatusReasonOperatorMissingPermissions
	default:
		return StatusReasonReconcileFailed
	}
}

// syntheticsWorkerFailureMessage describes a reconcile failure for the status and for the Kubernetes event.
func syntheticsWorkerFailureMessage(reason string, err error) string {
	if reason != StatusReasonOperatorMissingPermissions {
		return err.Error()
	}
	return fmt.Sprintf(
		"The API server rejected a synthetics-worker resource as forbidden. One possible cause is Kubernetes' "+
			"privilege escalation prevention (the operator can only grant permissions it holds itself). The API "+
			"server reported: %s",
		err.Error(),
	)
}

// createOrUpdateSyntheticsWorker creates or updates the resources of every configured synthetics-worker instance. The
// returned flag reports whether the reconciliation has been performed. The returned error is feature-wide (e.g. the
// orphan cleanup failed), not a single instance's misconfiguration, which is reported per-instance in the returned
// results instead and does not prevent the other instances from being reconciled.
func (m *SyntheticsWorkerManager) createOrUpdateSyntheticsWorker(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) ([]swresources.InstanceResult, bool, error) {
	results, err := m.syntheticsWorkerResourceManager.CreateOrUpdateSyntheticsWorkerResources(
		ctx,
		operatorConfigurationResource,
		logger,
	)
	if err != nil {
		if !errors.Is(err, swresources.ErrMisconfigured) {
			logger.Error(err, "failed to create/update the synthetics-worker resources")
		}
		return results, false, err
	}

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, result := range results {
		resourcesHaveBeenCreated = resourcesHaveBeenCreated || result.Created
		resourcesHaveBeenUpdated = resourcesHaveBeenUpdated || result.Updated
	}

	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("synthetics-worker Kubernetes resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("synthetics-worker Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("synthetics-worker Kubernetes resources have been updated.")
	} else {
		logger.Debug("synthetics-worker Kubernetes resources are already up to date, no changes required")
	}

	return results, true, nil
}

func (m *SyntheticsWorkerManager) removeSyntheticsWorker(
	ctx context.Context,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.syntheticsWorkerResourceManager.DeleteResources(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to delete the synthetics-worker Kubernetes resources, requeuing reconcile request.")
		return err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("synthetics-worker Kubernetes resources have been deleted.")
	} else {
		logger.Debug("no synthetics-worker Kubernetes resources to delete")
	}
	return nil
}
