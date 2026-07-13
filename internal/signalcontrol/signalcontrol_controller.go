// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/enablement"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	updateStatusFailedMessageSignalControl = "Failed to update Dash0 Signal Control status conditions, requeuing reconcile request."

	reasonSignalControlNotEnabledForOrganization = "SignalControlNotEnabledForOrganization"
	reasonSignalControlEnablementCheckFailed     = "SignalControlEnablementCheckFailed"
	reasonSignalControlNoDash0Export             = "NoDash0ExportConfigured"
)

// errNoDash0Export is returned from verifyEnablement to requeue the reconcile request when Signal Control is enabled
// but the operator configuration has no Dash0 export, so the resource recovers once a Dash0 export is configured.
var errNoDash0Export = errors.New("no Dash0 export configured in the operator configuration while Signal Control is enabled")

type SignalControlReconciler struct {
	client.Client
	signalControlManager *SignalControlManager
	collectorManager     *collectors.CollectorManager
	enablementChecker    *enablement.EnablementChecker
}

func NewSignalControlReconciler(
	k8sClient client.Client,
	signalControlManager *SignalControlManager,
	collectorManager *collectors.CollectorManager,
	enablementChecker *enablement.EnablementChecker,
) *SignalControlReconciler {
	return &SignalControlReconciler{
		Client:               k8sClient,
		signalControlManager: signalControlManager,
		collectorManager:     collectorManager,
		enablementChecker:    enablementChecker,
	}
}

func (r *SignalControlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("signalcontrolcontroller").
		For(&dash0v1alpha1.Dash0SignalControl{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *SignalControlReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Info("reconciling Signal Control resource triggered by watch event", "request", req)

	checkResourceResult, err := resources.VerifyThatUniqueNonDegradedResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0SignalControl{},
		updateStatusFailedMessageSignalControl,
		logger,
	)
	if err != nil {
		// temporary error, requeue
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		// Signal Control resource has been deleted: remove the Edge Proxy & reconcile the collector.
		hasBeenReconciled, reconcileErr := r.signalControlManager.ReconcileSignalControl(ctx, nil)
		if reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile Signal Control deletion")
			return reconcile.Result{}, reconcileErr
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled Signal Control resource deletion", "request", req)
		}
		if _, reconcileErr = r.collectorManager.ReconcileOpenTelemetryCollector(ctx); reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile collector resources after Signal Control deletion")
			return reconcile.Result{}, reconcileErr
		}
		return reconcile.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		// This resource has been marked as degraded (not the most recent one), skip reconciliation for it.
		return reconcile.Result{}, nil
	}

	signalControlResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0SignalControl)

	if _, err := resources.InitStatusConditions(
		ctx,
		r.Client,
		signalControlResource,
		signalControlResource.Status.Conditions,
		logger,
	); err != nil {
		return ctrl.Result{}, err
	}

	// When Signal Control is enabled via the resource, verify the organization is entitled to use it before applying
	// any Signal Control components to the collector or deploying the Edge Proxy.
	if signalControlResource.Spec.Enabled == nil || *signalControlResource.Spec.Enabled {
		if handled, err := r.verifyEnablement(ctx, signalControlResource, logger); handled {
			return reconcile.Result{}, err
		}
	}

	hasBeenReconciled, err := r.signalControlManager.ReconcileSignalControl(ctx, signalControlResource)
	if err != nil {
		signalControlResource.EnsureResourceIsMarkedAsDegraded(
			"ReconcileFailed",
			"Failed to reconcile Signal Control resource.",
		)
		if statusErr := r.Status().Update(ctx, signalControlResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageSignalControl)
		}
		logger.Error(err, "failed to reconcile Signal Control resource")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info("successfully reconciled Signal Control resource", "request", req)
	}

	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(ctx); err != nil {
		signalControlResource.EnsureResourceIsMarkedAsDegraded(
			"CollectorReconcileFailed",
			"Failed to reconcile collector resources after Signal Control change.",
		)
		if statusErr := r.Status().Update(ctx, signalControlResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageSignalControl)
		}
		logger.Error(err, "failed to reconcile collector resources after Signal Control change")
		return reconcile.Result{}, err
	}

	signalControlResource.EnsureResourceIsMarkedAsAvailable()
	if err := r.Status().Update(ctx, signalControlResource); err != nil {
		logger.Error(err, updateStatusFailedMessageSignalControl)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// verifyEnablement checks the preconditions for applying Signal Control: a Dash0 export must be configured in the
// operator configuration, and the organization must be entitled to use Signal Control. It returns handled=false when
// both hold, so the caller proceeds with the normal reconcile flow. It returns handled=true when Signal Control must
// not be applied (no Dash0 export, the organization is not entitled, or the entitlement could not be verified); in
// that case it has already removed the Signal Control components, reconciled the collector into a plain collector, and
// marked the resource as degraded, and the caller should stop reconciling and return the provided error. For the
// no-Dash0-export and unverifiable-entitlement cases it returns a non-nil error so the request is requeued and the
// resource recovers once the precondition is met (the controller does not watch the operator configuration).
func (r *SignalControlReconciler) verifyEnablement(
	ctx context.Context,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	logger logd.Logger,
) (bool, error) {
	operatorConfig, err := r.findOperatorConfigurationResource(ctx, logger)
	if err != nil {
		return true, err
	}

	// Signal Control requires a Dash0 export in the operator configuration for the Decision Maker auth token; without
	// it, Signal Control cannot function. Verify this before the entitlement check, which also depends on the export's
	// auth token. This covers cases the operator configuration validation webhook cannot (webhook bypass, or the
	// operator configuration being deleted entirely), the latter reaching here as a nil operator configuration.
	if operatorConfig == nil || !operatorConfig.HasDash0ExportConfigured() {
		message := "Signal Control is enabled, but the operator configuration has no Dash0 export. Signal Control " +
			"requires a Dash0 export with an auth token for the Decision Maker connection. Signal Control components " +
			"will not be added to the collector and the Edge Proxy will not be deployed until a Dash0 export is " +
			"configured."
		logger.WarnTelemetryCollectionIssue(message)
		// Requeue (return a non-nil error) so the resource recovers once a Dash0 export is configured: the controller
		// watches only the Signal Control resource, not the operator configuration, so without a requeue the resource
		// would stay degraded until the next change to the Signal Control resource or an operator restart.
		return r.disableSignalControlAsDegraded(
			ctx,
			signalControlResource,
			reasonSignalControlNoDash0Export,
			message,
			errNoDash0Export,
			logger,
		)
	}

	checkResult, checkErr := r.enablementChecker.Check(ctx, operatorConfig, logger)
	if checkResult == enablement.ResultAllowed {
		// The organization is entitled to use Signal Control; proceed with the normal reconcile flow.
		return false, nil
	}

	var reason, message string
	if checkResult == enablement.ResultNotAllowed {
		reason = reasonSignalControlNotEnabledForOrganization
		message = "The organization is not entitled to use Signal Control. Signal Control components will not be " +
			"added to the collector and the Edge Proxy will not be deployed."
		logger.WarnTelemetryCollectionIssue(message)
	} else {
		reason = reasonSignalControlEnablementCheckFailed
		message = "The Signal Control entitlement for the organization could not be verified. Signal Control " +
			"components will not be added to the collector and the Edge Proxy will not be deployed until the " +
			"entitlement is confirmed."
		logger.WarnTelemetryCollectionIssue(message, "error", checkErr)
	}

	// When the entitlement could not be verified (Unknown due to a check error), requeue to retry until the
	// entitlement is confirmed. A definitive NotAllowed does not requeue; it is re-evaluated on the next change to
	// the Signal Control resource.
	var requeueErr error
	if checkResult != enablement.ResultNotAllowed && checkErr != nil {
		requeueErr = checkErr
	}
	return r.disableSignalControlAsDegraded(ctx, signalControlResource, reason, message, requeueErr, logger)
}

// disableSignalControlAsDegraded removes the Edge Proxy and reconciles the collector so it drops the Signal Control
// components (rendering a plain collector), then marks the Signal Control resource as degraded with the given reason
// and message. It always returns handled=true. It returns any error from the teardown or status update; otherwise it
// returns the provided requeueErr (nil for a definitive negative verdict, non-nil to trigger a retry).
func (r *SignalControlReconciler) disableSignalControlAsDegraded(
	ctx context.Context,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	reason, message string,
	requeueErr error,
	logger logd.Logger,
) (bool, error) {
	if _, err := r.signalControlManager.ReconcileSignalControl(ctx, nil); err != nil {
		logger.Error(err, "failed to remove Signal Control components while disabling Signal Control")
		return true, err
	}
	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(ctx); err != nil {
		logger.Error(err, "failed to reconcile collector resources while disabling Signal Control")
		return true, err
	}

	signalControlResource.EnsureResourceIsMarkedAsDegraded(reason, message)
	if statusErr := r.Status().Update(ctx, signalControlResource); statusErr != nil {
		logger.Error(statusErr, updateStatusFailedMessageSignalControl)
		return true, statusErr
	}

	return true, requeueErr
}

func (r *SignalControlReconciler) findOperatorConfigurationResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	resource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		r.Client,
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
