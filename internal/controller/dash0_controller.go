// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
)

// const dash0Finalizer = "operator.dash0.com/finalizer"

const (
	conditionTypeAvailable = "Available"
	conditionTypeDegraded  = "Degraded"

	// requeueTimeAferProblem is the retry-time in case of problems that do not produce.
	requeueTimeAferProblem = 1 * time.Second
)

// Dash0Reconciler reconciles a Dash0 object
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

	// Check wheter the Dash0 custom resource exists.
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

	if dash0CustomResource.Status.Conditions == nil || len(dash0CustomResource.Status.Conditions) == 0 {
		// No status is available, assume unknown.
		meta.SetStatusCondition(&dash0CustomResource.Status.Conditions, metav1.Condition{Type: conditionTypeAvailable, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, dash0CustomResource); err != nil {
			log.Error(err, "Cannot update the status of the Dash0 custom resource, requeuing reconciliation request.")
			return ctrl.Result{}, err
		}

		// Re-fetch the Dash0 custom resource after updating the status. This also helps to avoid triggering
		// "the object has been modified, please apply your changes to the latest version and try again".
		if err := r.Get(ctx, req.NamespacedName, dash0CustomResource); err != nil {
			log.Error(err, "Failed to re-fetch the Dash0 custom resource after updating its status, requeuing reconciliation request.")
			return ctrl.Result{}, err
		}
	}

	// Add finalizers to the Dash0 custom resource.
	// if !controllerutil.ContainsFinalizer(dash0CustomResource, dash0Finalizer) {
	//	log.Info("Adding finalizer to the Dash0 custom resource")
	//	if ok := controllerutil.AddFinalizer(dash0CustomResource, dash0Finalizer); !ok {
	//		log.Error(err, "Failed to add finalizer to the Dash0 custom resource, requeuing reconciliation request.")
	//		return ctrl.Result{RequeueAfter: requeueTimeAferProblem}, nil
	//	}
	//
	//	if err = r.Update(ctx, dash0CustomResource); err != nil {
	//		log.Error(err, "Failed to update custom resource to add finalizer, requeuing reconciliation request.")
	//		return ctrl.Result{}, err
	//	}
	// }

	// If a deletion timestamp is set this indicates that the Dash0 custom resource is about to be deleted.
	isMarkedForDeletion := dash0CustomResource.GetDeletionTimestamp() != nil
	if isMarkedForDeletion {
		// TODO proper cleanup needs to happen here (removeDash0ModificationsFromResources)
		//if controllerutil.ContainsFinalizer(dash0CustomResource, dash0Finalizer) {
		//	result, err2, done := r.handleFinalizerOperations(ctx, req, log, dash0CustomResource, err)
		//	if done {
		//		return result, err2
		//	}
		//}
		return ctrl.Result{}, nil
	}

	// TODO inject Dash0 instrumentations into _existint_ resources (later)

	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Failed to update Dash0 status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

//func (r *Dash0Reconciler) handleFinalizerOperations(ctx context.Context, req ctrl.Request, log logr.Logger, dash0 *operatorv1alpha1.Dash0, err error) (ctrl.Result, error, bool) {
//	log.Info("Performing Finalizer Operations for Dash0 before delete custom resource")
//
//	meta.SetStatusCondition(&dash0.Status.Conditions, metav1.Condition{Type: conditionTypeDegraded,
//		Status: metav1.ConditionUnknown, Reason: "Finalizing",
//		Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", dash0.Name)})
//
//	if err := r.Status().Update(ctx, dash0); err != nil {
//		log.Error(err, "Failed to update Dash0 status")
//		return ctrl.Result{}, err, true
//	}
//
//	// Perform all operations required before remove the finalizer and allow
//	// the Kubernetes API to remove the custom resource.
//	r.doFinalizerOperationsForDash0(dash0)
//
//	// TODO(user): If you add operations to the doFinalizerOperationsForDash0 method
//	// then you need to ensure that all worked fine before deleting and updating the Downgrade status
//	// otherwise, you should requeue here.
//
//	// Re-fetch the Dash0 Custom Resource before update the status
//	// so that we have the latest state of the resource on the cluster and we will avoid
//	// raise the issue "the object has been modified, please apply
//	// your changes to the latest version and try again" which would re-trigger the reconciliation
//	if err := r.Get(ctx, req.NamespacedName, dash0); err != nil {
//		log.Error(err, "Failed to re-fetch dash0")
//		return ctrl.Result{}, err, true
//	}
//
//	meta.SetStatusCondition(&dash0.Status.Conditions, metav1.Condition{Type: conditionTypeDegraded,
//		Status: metav1.ConditionTrue, Reason: "Finalizing",
//		Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", dash0.Name)})
//
//	if err := r.Status().Update(ctx, dash0); err != nil {
//		log.Error(err, "Failed to update Dash0 status")
//		return ctrl.Result{}, err, true
//	}
//
//	//log.Info("Removing Finalizer for Dash0 after successfully perform the operations")
//	//if ok := controllerutil.RemoveFinalizer(dash0, dash0Finalizer); !ok {
//	//	log.Error(err, "Failed to remove finalizer for Dash0")
//	//	return ctrl.Result{RequeueAfter: requeueTimeAferProblem}, nil, true
//	//}
//
//	if err := r.Update(ctx, dash0); err != nil {
//		log.Error(err, "Failed to remove finalizer for Dash0")
//		return ctrl.Result{}, err, true
//	}
//	return ctrl.Result{}, nil, false
//}

// finalizeDash0 will perform the required operations before delete the CR.
//func (r *Dash0Reconciler) doFinalizerOperationsForDash0(cr *operatorv1alpha1.Dash0) {
//	// TODO(user): Add the cleanup steps that the operator needs to do before the CR can be deleted,
//	// or remove this.
//
//	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
//	// created and managed in the reconciliation. These resources, such as the Deployment created on reconcile,
//	// are defined as depending on the custom resource, via an ownerRef, thus they are deleted automatically.
//
//	// The following implementation will raise an event
//	r.Recorder.Event(cr, "Warning", "Deleting",
//		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
//			cr.Name,
//			cr.Namespace))
//}
