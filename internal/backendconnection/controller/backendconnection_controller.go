// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	backendconnectionv1alpha1 "github.com/dash0hq/dash0-operator/api/backendconnection/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/util"
	"github.com/dash0hq/dash0-operator/internal/common/controller"
)

// BackendConnectionReconciler reconciles a BackendConnection object
type BackendConnectionReconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
}

const (
	updateStatusFailedMessage = "Failed to update Dash0 backend connection status conditions, requeuing reconcile request."
)

// SetupWithManager sets up the controller with the Manager.
func (r *BackendConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backendconnectionv1alpha1.BackendConnection{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=operator.dash0.com,resources=backendconnections,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=backendconnections/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.dash0.com,resources=backendconnections/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *BackendConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for backend connection resource")

	namespace := req.Namespace

	namespaceStillExists, err := controller.CheckIfNamespaceExists(ctx, r.ClientSet, namespace, &logger)
	if err != nil {
		// The error has already been logged in checkIfNamespaceExists.
		return ctrl.Result{}, err
	} else if !namespaceStillExists {
		logger.Info("The namespace seems to have been deleted after this reconcile request has been scheduled. " +
			"Ignoring the reconcile request.")
		return ctrl.Result{}, nil
	}

	customResource, stopReconcile, err :=
		controller.VerifyUniqueCustomResourceExists(
			ctx,
			r.Client,
			r.Status(),
			&backendconnectionv1alpha1.BackendConnection{},
			updateStatusFailedMessage,
			req,
			logger,
		)
	if err != nil {
		return ctrl.Result{}, err
	} else if stopReconcile {
		return ctrl.Result{}, nil
	}
	backendConnectionResource := customResource.(*backendconnectionv1alpha1.BackendConnection)

	_, err = controller.InitStatusConditions(
		ctx,
		r.Status(),
		backendConnectionResource,
		backendConnectionResource.Status.Conditions,
		string(util.ConditionTypeAvailable),
		&logger,
	)
	if err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	isMarkedForDeletion, runCleanupActions, err := controller.CheckImminentDeletionAndHandleFinalizers(
		ctx,
		r.Client,
		backendConnectionResource,
		backendconnectionv1alpha1.FinalizerId,
		&logger,
	)
	if err != nil {
		// The error has already been logged in checkImminentDeletionAndHandleFinalizers
		return ctrl.Result{}, err
	} else if runCleanupActions {
		err = r.runCleanupActions(ctx, backendConnectionResource, &logger)
		if err != nil {
			// error has already been logged in runCleanupActions
			return ctrl.Result{}, err
		}
		// The Dash0 backend connection resource is slated for deletion, all cleanup actions (removing the OTel
		// collector resources) have been processed, no further reconciliation is necessary.
		return ctrl.Result{}, nil
	} else if isMarkedForDeletion {
		// The Dash0 backend connection resource is slated for deletion, the finalizer has already been removed (which
		// means all cleanup actions have been processed), no further reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	isNew, isChanged, err := r.createOrUpdateOpenTelemetryCollectorResources(ctx, namespace, backendConnectionResource, &logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	backendConnectionResource.EnsureResourceIsMarkedAsAvailable(isNew, isChanged)
	if err = r.Status().Update(ctx, backendConnectionResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BackendConnectionReconciler) createOrUpdateOpenTelemetryCollectorResources(
	ctx context.Context,
	namespace string,
	backendConnectionResource *backendconnectionv1alpha1.BackendConnection,
	logger *logr.Logger,
) (bool, bool, error) {
	return true, false, nil
}

func (r *BackendConnectionReconciler) runCleanupActions(
	ctx context.Context,
	backendConnectionResource *backendconnectionv1alpha1.BackendConnection,
	logger *logr.Logger,
) error {
	return nil
}
