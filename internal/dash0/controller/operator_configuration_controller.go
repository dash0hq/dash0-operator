// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
	"github.com/go-logr/logr"
)

const (
	ManagerContainerName = "manager"
)

type OperatorConfigurationReconciler struct {
	client.Client
	Clientset               *kubernetes.Clientset
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	DeploymentSelfReference *appsv1.Deployment
	DanglingEventsTimeouts  *DanglingEventsTimeouts
}

func (r *OperatorConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DanglingEventsTimeouts == nil {
		r.DanglingEventsTimeouts = defaultDanglingEventsTimeouts
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0OperatorConfiguration{}).
		Complete(r)
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;list;patch;update
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete
//+kubebuilder:rbac:groups=operator.dash0monitoring.com,resources=dash0operatorconfigurations,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.dash0monitoring.com,resources=dash0operatorconfigurations/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.dash0monitoring.com,resources=dash0operatorconfigurations/status,verbs=get;update;patch

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
func (r *OperatorConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for Dash0Operator resource")

	resource, stopReconcile, err := verifyUniqueOperatorConfigurationResourceExists(
		ctx,
		r.Client,
		r.Status(),
		"Failed to update Dash0 operator status conditions, requeuing reconcile request.",
		req,
		logger,
	)
	if err != nil {
		return ctrl.Result{}, err
	} else if stopReconcile {
		return ctrl.Result{}, nil
	}

	if _, err := util.InitStatusConditions(
		ctx,
		r.Status(),
		resource,
		&logger,
	); err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	resource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return ctrl.Result{}, err
	}

	isSelfMonitoringEnabled, err := IsSelfMonitoringEnabled(r.DeploymentSelfReference)
	if err != nil {
		return ctrl.Result{}, err
	}

	shouldEnableSelfMonitoring := resource.Spec.SelfMonitoring.Enabled
	if shouldEnableSelfMonitoring == isSelfMonitoringEnabled {
		return ctrl.Result{}, nil
	}

	var updatedControllerDeployment *appsv1.Deployment
	if shouldEnableSelfMonitoring {
		updatedControllerDeployment, err = EnableSelfMonitoring(r.DeploymentSelfReference, resource.Spec.IngressEndpoint, resource.Spec.AuthorizationToken)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		updatedControllerDeployment, err = DisableSelfMonitoring(r.DeploymentSelfReference)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if err = r.Client.Update(ctx, updatedControllerDeployment); err != nil {
		// We could not set up the self-monitoring, the operator resource must be marked as degraded
		resource.EnsureResourceIsMarkedAsDegraded("CannotApplySelfMonitoring", "Could not update the controller deployment to reflect the self-monitoring settings")
		if statusUpdateErr := r.Status().Update(ctx, resource); statusUpdateErr != nil {
			logger.Error(statusUpdateErr, "Failed to update Dash0 operator status conditions, requeuing reconcile request.")
			return ctrl.Result{}, statusUpdateErr
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// verifyUniqueOperatorResourceExists loads the resource that the current reconcile request applies to, if it
// exists. It also checks whether there is only one such resource in the cluster. The bool returned has the meaning
// "stop the reconcile request", that is, if the function returns true, it expects the caller to stop the reconcile
// request immediately and not requeue it. If an error occurs during any of the checks (for example while talking to
// the Kubernetes API server), the function will return that error, the caller should then ignore the bool result
// and requeue the reconcile request.
//
//   - If the resource does not exist, the function logs a message and returns (nil, true, nil) and expects the caller
//     to stop the reconciliation (without requeing it).
//   - If there are multiple resources in the namespace, but the given resource is the most recent one, the function
//     will return the resource together with (false, nil) as well, since the newest resource should be reconciled. The
//     caller should continue with the reconcile request in that case.
//   - If there are multiple resources and the given one is not the most recent one, the function will return true for
//     stopReconcile and the caller is expected to stop the reconcile and not requeue it.
//   - If any error is encountered when searching for resources etc., that error will be returned, the caller is
//     expected to ignore the bool result and requeue the reconcile request.
func verifyUniqueOperatorConfigurationResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	statusWriter client.SubResourceWriter,
	updateStatusFailedMessage string,
	req ctrl.Request,
	logger logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, bool, error) {
	dash0OperatorResource, stopReconcile, err := verifyThatOperatorConfigurationResourceExists(
		ctx,
		k8sClient,
		req,
		&logger,
	)
	if err != nil || stopReconcile {
		return nil, stopReconcile, err
	}
	stopReconcile, err =
		verifyThatOperatorConfigurationResourceIsUniqueInCluster(
			ctx,
			k8sClient,
			statusWriter,
			dash0OperatorResource,
			updateStatusFailedMessage,
			&logger,
		)
	return dash0OperatorResource, stopReconcile, err
}

// verifyThatOperatorResourceExists loads the resource that the current reconcile request applies to. If that
// resource does not exist, the function logs a message and returns (nil, true, nil) and expects the caller to stop the
// reconciliation (without requeing it). If any other error occurs while trying to fetch the resource, the function logs
// the error and returns (nil, true, err) and expects the caller to requeue the reconciliation.
func verifyThatOperatorConfigurationResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, bool, error) {
	resource := &dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
		},
	}

	if err := k8sClient.Get(ctx, types.NamespacedName{Name: req.Name}, resource); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				"The Dash0 monitoring resource has not been found, either it hasn't been installed or it has " +
					"been deleted. Ignoring the reconcile request.")
			// stop the reconciliation, and do not requeue it (that is, return (ctrl.Result{}, nil))
			return nil, true, nil
		}
		logger.Error(err, "Failed to get the Dash0 monitoring resource, requeuing reconcile request.")
		// requeue the reconciliation (that is, return (ctrl.Result{}, err))
		return nil, true, err
	}

	return resource, false, nil
}

// verifyThatOperatorResourceIsUniqueInCluster checks whether there are any additional resources of the same type
// in the namespace, besides the one that the current reconcile request applies to. The bool the function returns has
// the semantic stopReconcile, that is, if the function returns true, it expects the caller to stop the reconcile. If
// there are no errors and the resource is unique, the function will return (false, nil). If there are multiple
// resources in the namespace, but the given resource is the most recent one, the function will return (false, nil) as
// well, since the newest resource should be reconciled. If there are multiple resources and the given one is not the
// most recent one, the function will return (true, nil), and the caller is expected to stop the reconcile and not
// requeue it.
// If any error is encountered when searching for other resource etc., that error will be returned, the caller is
// expected to ignore the bool result and requeue the reconcile request.
func verifyThatOperatorConfigurationResourceIsUniqueInCluster(
	ctx context.Context,
	k8sClient client.Client,
	statusWriter client.SubResourceWriter,
	resource *dash0v1alpha1.Dash0OperatorConfiguration,
	updateStatusFailedMessage string,
	logger *logr.Logger,
) (bool, error) {
	allCustomResourcesInCluster := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := k8sClient.List(
		ctx,
		allCustomResourcesInCluster,
	); err != nil {
		logger.Error(
			err,
			"Failed to list all Dash0 operator resources, requeuing reconcile request.",
		)
		return true, err
	}

	items := allCustomResourcesInCluster.Items
	if len(items) > 1 {
		// There are multiple instances of the Dash0 operator resource in this cluster. If the resource that is
		// currently being reconciled is the one that has been most recently created, we assume that this is the source
		// of truth in terms of configuration settings etc., and we ignore the other instances in this reconcile request
		// (they will be handled when they are being reconciled). If the currently reconciled resource is not the most
		// recent one, we set its status to degraded.
		sort.Sort(util.SortOperatorConfigurationByCreationTimestamp(items))
		mostRecentResource := items[len(items)-1]
		if mostRecentResource.UID == resource.UID {
			logger.Info(
				"At least one other Dash0 operator resource exists in this operator. This Dash0 operator " +
					"resource is the most recent one. The state of the other resource(s) will be set to degraded.",
			)
			// continue with the reconcile request for this resource, let the reconcile requests for the other offending
			// resources handle the situation for those resources
			return false, nil
		} else {
			logger.Info(
				"At least one other Dash0 operator resource exists in this cluster, and at least one other "+
					"Dash0 operator resource has been created more recently than this one. Setting the state of "+
					"this resource to degraded.",
				"most recently created Dash0 operator resource",
				fmt.Sprintf("%s (%s)", mostRecentResource.Name, mostRecentResource.UID),
			)
			resource.EnsureResourceIsMarkedAsDegraded(
				"NewerResourceIsPresent",
				"There is a more recently created Dash0 operator resource in this cluster, please remove all but one resource instance.",
			)
			if err := statusWriter.Update(ctx, resource); err != nil {
				logger.Error(err, updateStatusFailedMessage)
				return true, err
			}
			// stop the reconciliation, and do not requeue it
			return true, nil
		}
	}
	return false, nil
}
