// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	backendconnectionv1alpha1 "github.com/dash0hq/dash0-operator/api/backendconnection/v1alpha1"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/util"
	"github.com/dash0hq/dash0-operator/internal/common/controller"
)

// BackendConnectionReconciler reconciles a BackendConnection object
type BackendConnectionReconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
	*otelcolresources.OTelColResourceManager
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
		err = r.runCleanupActions(ctx, namespace, backendConnectionResource, &logger)
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

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		r.OTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			namespace,
			backendConnectionResource.Spec.IngressEndpoint,
			backendConnectionResource.Spec.AuthorizationToken,
			backendConnectionResource.Spec.SecretRef,
			&logger,
		)
	if err != nil {
		return ctrl.Result{}, err
	}

	backendConnectionResource.EnsureResourceIsMarkedAsAvailable(resourcesHaveBeenCreated, resourcesHaveBeenUpdated)
	if err = r.Status().Update(ctx, backendConnectionResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BackendConnectionReconciler) runCleanupActions(
	ctx context.Context,
	namespace string,
	backendConnectionResource *backendconnectionv1alpha1.BackendConnection,
	logger *logr.Logger,
) error {
	if err := r.OTelColResourceManager.DeleteResources(
		ctx,
		namespace,
		backendConnectionResource.Spec.IngressEndpoint,
		backendConnectionResource.Spec.AuthorizationToken,
		backendConnectionResource.Spec.SecretRef,
		logger,
	); err != nil {
		logger.Error(err, "Failed to delete the OpenTelemetry collector resources, requeuing reconcile request.")
		return err
	}

	backendConnectionResource.EnsureResourceIsMarkedAsAboutToBeDeleted()
	if err := r.Status().Update(ctx, backendConnectionResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return err
	}

	controllerutil.RemoveFinalizer(backendConnectionResource, backendconnectionv1alpha1.FinalizerId)
	if err := r.Update(ctx, backendConnectionResource); err != nil {
		logger.Error(err, "Failed to remove the finalizer from the backend connection resource, requeuing reconcile request.")
		return err
	}
	return nil
}

func EnsureBackendConnectionResourceInDash0OperatorNamespace(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	ingressEndpoint string,
	authorizationToken string,
	secretRef string,
) error {
	logger := log.FromContext(ctx)

	list := &backendconnectionv1alpha1.BackendConnectionList{}
	err := k8sClient.List(
		ctx,
		list,
		&client.ListOptions{
			Namespace: operatorNamespace,
		},
	)

	if err != nil {
		return err
	}
	if len(list.Items) > 0 {
		// There is a backend connection resource in the dash0 namespace, all is peachy.
		return nil
	}

	if ingressEndpoint == "" {
		err = fmt.Errorf("no ingress endpoint provided, unable to create the OpenTelemetry collector")
		logger.Error(err, "failed to create a backend connection, no telemetry will be reported to Dash0")
		return err
	}
	if authorizationToken == "" && secretRef == "" {
		err = fmt.Errorf("neither an authorization token nor a reference to a Kubernetes secret has been provided, " +
			"unable to create the OpenTelemetry collector")
		logger.Error(err, "failed to create a backend connection, no telemetry will be reported to Dash0")
		return err
	}

	// There is no backend connection resource in the dash0 namespace yet, create one, so that a default OTel collector
	// is created
	logger.Info(fmt.Sprintf("Creating a backend connection resource in the Dash0 operator namespace %s.", operatorNamespace))
	return k8sClient.Create(ctx, &backendconnectionv1alpha1.BackendConnection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backendconnectionv1alpha1.DefaultName,
			Namespace: operatorNamespace,
		},
		Spec: backendconnectionv1alpha1.BackendConnectionSpec{
			IngressEndpoint:    ingressEndpoint,
			AuthorizationToken: authorizationToken,
			SecretRef:          secretRef,
		},
	})
}

func RemoveBackendConnectionIfNoDash0CustomResourceIsLeft(
	ctx context.Context,
	operatorNamespace string,
	k8sClient client.Client,
	dash0CustomResourceToBeDeleted *dash0v1alpha1.Dash0,
) error {
	logger := log.FromContext(ctx)
	list := &dash0v1alpha1.Dash0List{}
	err := k8sClient.List(
		ctx,
		list,
	)

	if err != nil {
		logger.Error(err, "Error when checking whether there are any Dash0 custom resources left in the cluster.")
		return err
	}
	if len(list.Items) > 1 {
		// There is still more than one Dash0 custom resource in the namespace, do not remove the backend connection.
		return nil
	}

	if len(list.Items) == 1 && list.Items[0].UID != dash0CustomResourceToBeDeleted.UID {
		// There is only one Dash0 custom resource left, but it is *not* the one that is about to be deleted.
		// Do not remove the backend connection.
		logger.Info(
			"There is only one Dash0 custom resource left, but it is not the one being deleted.",
			"to be deleted/UID",
			dash0CustomResourceToBeDeleted.UID,
			"to be deleted/namespace",
			dash0CustomResourceToBeDeleted.Namespace,
			"to be deleted/name",
			dash0CustomResourceToBeDeleted.Name,
			"existing resource/UID",
			list.Items[0].UID,
			"existing resource/namespace",
			list.Items[0].Namespace,
			"existing resource/name",
			list.Items[0].Name,
		)
		return nil
	}

	// Either there is no Dash0 custom resource left, or only one and that one is about to be deleted. Delete the
	// backend connection.
	logger.Info(fmt.Sprintf("Deleting the backend connection resource in the Dash0 operator namespace %s.", operatorNamespace))
	return k8sClient.Delete(ctx, &backendconnectionv1alpha1.BackendConnection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backendconnectionv1alpha1.DefaultName,
			Namespace: operatorNamespace,
		},
	})
}
