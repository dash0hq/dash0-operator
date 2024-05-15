// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
)

type Dash0Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *Dash0Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Dash0{}).
		// Watches(&source.Kind{Type: &appsv1.Deployment{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfApplicable)).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *Dash0Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("namespace", req.NamespacedName.Namespace, "name", req.NamespacedName.Name)

	// Check whether the Dash0 custom resource exists.
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	err := r.Get(ctx, req.NamespacedName, dash0CustomResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("The Dash0 custom resource has not been found, either it hasn't been installed or it has been deleted. Ignoring the reconciliation request.")
			// stop the reconciliation
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get the Dash0 custom resource, requeuing reconciliation request.")
		// requeue the request.
		return ctrl.Result{}, err
	}

	needsRefresh := false
	if dash0CustomResource.Status.Conditions == nil || len(dash0CustomResource.Status.Conditions) == 0 {
		setAvailableConditionToUnknown(dash0CustomResource)
		needsRefresh = true
	} else if availableCondition := meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(operatorv1alpha1.ConditionTypeAvailable)); availableCondition == nil {
		setAvailableConditionToUnknown(dash0CustomResource)
		needsRefresh = true
	}
	if needsRefresh {
		err = r.refreshStatus(ctx, dash0CustomResource, req, log)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// If a deletion timestamp is set this indicates that the Dash0 custom resource is about to be deleted.
	isMarkedForDeletion := dash0CustomResource.GetDeletionTimestamp() != nil
	if isMarkedForDeletion {
		return ctrl.Result{}, nil
	}

	// TODO inject Dash0 instrumentations into _existing_ resources (later)

	makeAvailable(dash0CustomResource)
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Failed to update Dash0 status conditions, requeuing reconciliation request.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Dash0Reconciler) refreshStatus(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	req ctrl.Request,
	log logr.Logger,
) error {
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Cannot update the status of the Dash0 custom resource, requeuing reconciliation request.")
		return err
	}
	// Re-fetch the Dash0 custom resource after updating the status. This also helps to avoid triggering
	// "the object has been modified, please apply your changes to the latest version and try again".
	if err := r.Get(ctx, req.NamespacedName, dash0CustomResource); err != nil {
		log.Error(err, "Failed to re-fetch the Dash0 custom resource after updating its status, requeuing reconciliation request.")
		return err
	}
	return nil
}
