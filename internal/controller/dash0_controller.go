// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/k8sresources"
	. "github.com/dash0hq/dash0-operator/internal/util"
)

type Dash0Reconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Versions  k8sresources.Versions
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
	log := log.FromContext(ctx)
	log.Info("Processig reconcile request")

	// Check whether the Dash0 custom resource exists.
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	err := r.Get(ctx, req.NamespacedName, dash0CustomResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("The Dash0 custom resource has not been found, either it hasn't been installed or it has been deleted. Ignoring the reconcile request.")
			// stop the reconciliation
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get the Dash0 custom resource, requeuing reconcile request.")
		// requeue the request.
		return ctrl.Result{}, err
	}

	isFirstReconcile, err := r.initStatusConditions(ctx, dash0CustomResource, &log)
	if err != nil {
		return ctrl.Result{}, err
	}

	isMarkedForDeletion := r.handleFinalizers(dash0CustomResource)
	if isMarkedForDeletion {
		// Dash0 is marked for deletion, no further reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	if isFirstReconcile {
		r.handleFirstReconcile(ctx, dash0CustomResource, &log)
	}

	ensureResourceIsMarkedAsAvailable(dash0CustomResource)
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Failed to update Dash0 status conditions, requeuing reconcile request.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Dash0Reconciler) initStatusConditions(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) (bool, error) {
	firstReconcile := false
	needsRefresh := false
	if dash0CustomResource.Status.Conditions == nil || len(dash0CustomResource.Status.Conditions) == 0 {
		setAvailableConditionToUnknown(dash0CustomResource)
		firstReconcile = true
		needsRefresh = true
	} else if availableCondition := meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(operatorv1alpha1.ConditionTypeAvailable)); availableCondition == nil {
		setAvailableConditionToUnknown(dash0CustomResource)
		needsRefresh = true
	}
	if needsRefresh {
		err := r.refreshStatus(ctx, dash0CustomResource, log)
		if err != nil {
			return firstReconcile, err
		}
	}
	return firstReconcile, nil
}

func (r *Dash0Reconciler) handleFinalizers(dash0CustomResource *operatorv1alpha1.Dash0) bool {
	isMarkedForDeletion := dash0CustomResource.GetDeletionTimestamp() != nil
	// if !isMarkedForDeletion {
	//   add finalizers here
	// } else /* if has finalizers */ {
	//   execute finalizers here
	// }
	return isMarkedForDeletion
}

func (r *Dash0Reconciler) handleFirstReconcile(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) {
	log.Info("Initial reconcile in progress.")
	instrumentationEnabled := true
	instrumentingExistingResourcesEnabled := true
	if !instrumentationEnabled {
		log.Info(
			"Instrumentation is not enabled, neither new nor existing resources will be modified to send telemetry to Dash0.",
		)
		return
	}

	if !instrumentingExistingResourcesEnabled {
		log.Info(
			"Instrumenting existing resources is not enabled, only new resources will be modified (at deploy time) to send telemetry to Dash0.",
		)
		return
	}

	log.Info("Modifying existing resources to make them send telemetry to Dash0.")
	if err := r.modifyExistingResources(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Modifying existing resources failed.")
	}
}

func (r *Dash0Reconciler) refreshStatus(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) error {
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Cannot update the status of the Dash0 custom resource, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) modifyExistingResources(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0) error {
	namespace := dash0CustomResource.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("!%s", Dash0AutoInstrumentationLabel),
	}

	deploymentsInNamespace, err := r.ClientSet.AppsV1().Deployments(namespace).List(ctx, listOptions)
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}

	for _, deployment := range deploymentsInNamespace.Items {
		logger := log.FromContext(ctx).WithValues("resource type", "deployment", "resource namespace", deployment.GetNamespace(), "resource name", deployment.GetName())
		operationLabel := "Modifying deployment"
		err := Retry(operationLabel, func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: deployment.GetNamespace(),
				Name:      deployment.GetName(),
			}, &deployment); err != nil {
				return fmt.Errorf("error when fetching deployment %s/%s: %w", deployment.GetNamespace(), deployment.GetName(), err)
			}
			hasBeenModified := k8sresources.ModifyDeployment(
				&deployment,
				deployment.GetNamespace(),
				r.Versions,
				logger,
			)
			if hasBeenModified {
				return r.Client.Update(ctx, &deployment)
			} else {
				return nil
			}
		}, &logger)

		if err != nil {
			QueueFailedInstrumentationEvent(r.Recorder, &deployment, "controller", err)
			return fmt.Errorf("Error when modifying deployment %s/%s: %w", deployment.GetNamespace(), deployment.GetName(), err)
		} else {
			QueueSuccessfulInstrumentationEvent(r.Recorder, &deployment, "controller")
			logger.Info("Added instrumentation to deployment")
		}
	}
	return nil
}
