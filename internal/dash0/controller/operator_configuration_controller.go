// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type OperatorConfigurationReconciler struct {
	client.Client
	Clientset                    *kubernetes.Clientset
	PersesDashboardCrdReconciler *PersesDashboardCrdReconciler
	Scheme                       *runtime.Scheme
	Recorder                     record.EventRecorder
	DeploymentSelfReference      *appsv1.Deployment
	DanglingEventsTimeouts       *util.DanglingEventsTimeouts
	Images                       util.Images
	DevelopmentMode              bool
}

const (
	ControllerContainerName                        = "manager"
	updateStatusFailedMessageOperatorConfiguration = "Failed to update Dash0 operator configuration status " +
		"conditions, requeuing reconcile request."
)

var (
	operatorReconcileRequestMetric otelmetric.Int64Counter
)

func (r *OperatorConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DanglingEventsTimeouts == nil {
		r.DanglingEventsTimeouts = defaultDanglingEventsTimeouts
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0OperatorConfiguration{}).
		Complete(r)
}

func (r *OperatorConfigurationReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "operatorconfiguration.reconcile_requests")
	var err error
	if operatorReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for operator configuration resource reconcile requests"),
	); err != nil {
		logger.Error(err, "Cannot initialize the metric %s.")
	}
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;list;patch;update
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0operatorconfigurations,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0operatorconfigurations/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0operatorconfigurations/status,verbs=get;update;patch

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
	if operatorReconcileRequestMetric != nil {
		operatorReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)

	var resource *dash0v1alpha1.Dash0OperatorConfiguration
	resourceDeleted := false
	checkResourceResult, err := util.VerifyThatResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		&logger,
	)
	if checkResourceResult.ResourceDoesNotExist {
		resourceDeleted = true
	} else if err != nil {
		return ctrl.Result{}, err
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	if !resourceDeleted {
		resource = checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)
		stopReconcile, err :=
			util.VerifyThatResourceIsUniqueInScope(
				ctx,
				r.Client,
				req,
				resource,
				updateStatusFailedMessageOperatorConfiguration,
				&logger,
			)
		if err != nil {
			// Cannot validate whether this resource is normative, requeuing
			return ctrl.Result{}, err
		} else if stopReconcile {
			return ctrl.Result{}, nil
		}
		logger.Info("Reconciling the operator configuration resource", "name", req.Name)
	}

	if resourceDeleted {
		logger.Info("Reconciling the deletion of the operator configuration resource", "name", req.Name)
		if err = r.removeSelfMonitoringAndApiAccessAndUpdate(ctx); err != nil {
			logger.Error(err, "cannot disable self-monitoring/API access of the controller deployment, requeuing reconcile request.")
			return ctrl.Result{
				Requeue: true,
			}, nil
		} else {
			logger.Info("Self-monitoring of the controller deployment has been disabled")
		}
		return ctrl.Result{}, nil
	}

	if _, err = util.InitStatusConditions(
		ctx,
		r.Client,
		resource,
		resource.Status.Conditions,
		&logger,
	); err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	if resource.HasDash0ApiAccessConfigured() {
		dataset := resource.Spec.Export.Dash0.Dataset
		if dataset == "" {
			dataset = "default"
		}
		r.PersesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
			Endpoint: resource.Spec.Export.Dash0.ApiEndpoint,
			Dataset:  dataset,
		})
	} else {
		logger.Info("Settings required for managing dashboards via the operator are missing, the operator will not " +
			"update dashboards in Dash0.")
		r.PersesDashboardCrdReconciler.RemoveApiEndpointAndDataset()
	}

	currentSelfMonitoringAndApiAccessConfiguration, err :=
		selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
			r.DeploymentSelfReference,
			ControllerContainerName,
		)
	if err != nil {
		logger.Error(err, "cannot get self-monitoring/API access configuration from controller deployment")
		return ctrl.Result{
			Requeue: true,
		}, err
	}

	newSelfMonitoringAndApiAccessConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(resource, &logger)
	if err != nil {
		logger.Error(
			err,
			"cannot generate self-monitoring/API access configuration from operator configuration resource",
		)
		return ctrl.Result{}, err
	}

	controllerDeployment := &appsv1.Deployment{}
	if err = r.Client.Get(ctx, client.ObjectKeyFromObject(r.DeploymentSelfReference), controllerDeployment); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot fetch the current controller deployment: %w", err)
	}
	if !reflect.DeepEqual(currentSelfMonitoringAndApiAccessConfiguration, newSelfMonitoringAndApiAccessConfiguration) {
		if err = r.applySelfMonitoringAndApiAccess(
			controllerDeployment,
			newSelfMonitoringAndApiAccessConfiguration,
		); err != nil {
			logger.Error(err, "cannot apply self-monitoring configuration to the controller deployment")
			if statusUpdateErr := r.markAsDegraded(
				ctx,
				resource,
				"CannotUpdatedControllerDeployment",
				"Could not update the controller deployment to reflect the self-monitoring settings.",
				&logger,
			); statusUpdateErr != nil {
				return ctrl.Result{}, statusUpdateErr
			}
			return ctrl.Result{}, err
		}

		if err = r.Client.Update(ctx, controllerDeployment); err != nil {
			logger.Error(err, "cannot update the controller deployment")
			if statusUpdateErr := r.markAsDegraded(
				ctx,
				resource,
				"CannotUpdatedControllerDeployment",
				"Could not update the controller deployment.",
				&logger,
			); statusUpdateErr != nil {
				return ctrl.Result{}, statusUpdateErr
			}
			return ctrl.Result{}, err
		}
		logger.Info("The controller deployment has been updated.")
	} else {
		logger.Info("The controller deployment is up to date.")
	}

	resource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, updateStatusFailedMessageOperatorConfiguration)
		return ctrl.Result{}, fmt.Errorf("cannot mark the Dash0 operator configuration resource as available: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *OperatorConfigurationReconciler) applySelfMonitoringAndApiAccess(
	controllerDeployment *appsv1.Deployment,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
) error {
	if selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		if err := selfmonitoringapiaccess.EnableSelfMonitoringInControllerDeployment(
			controllerDeployment,
			ControllerContainerName,
			selfMonitoringAndApiAccessConfiguration,
			r.Images.GetOperatorVersion(),
			r.DevelopmentMode,
		); err != nil {
			return fmt.Errorf("cannot apply settings to enable self-monitoring to the controller deployment: %w", err)
		}
	} else if selfMonitoringAndApiAccessConfiguration.HasDash0ApiAccessConfigured() {
		if err := selfmonitoringapiaccess.UpdateApiTokenWithoutAddingSelfMonitoringToControllerDeployment(
			controllerDeployment,
			ControllerContainerName,
			selfMonitoringAndApiAccessConfiguration.GetDash0Authorization(),
		); err != nil {
			return fmt.Errorf("cannot add the Dash0 API token to the controller deployment: %w", err)
		}
		if err := selfmonitoringapiaccess.DisableSelfMonitoringInControllerDeployment(
			controllerDeployment,
			ControllerContainerName,
			false,
		); err != nil {
			return fmt.Errorf("cannot apply settings to disable self-monitoring to the controller deployment: %w", err)
		}
	} else {
		if err := selfmonitoringapiaccess.DisableSelfMonitoringInControllerDeployment(
			controllerDeployment,
			ControllerContainerName,
			true,
		); err != nil {
			return fmt.Errorf("cannot apply settings to the controller deployment to disable self-monitoring and API access: %w", err)
		}
	}

	return nil
}

func (r *OperatorConfigurationReconciler) removeSelfMonitoringAndApiAccessAndUpdate(ctx context.Context) error {
	updatedDeployment := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(r.DeploymentSelfReference), updatedDeployment); err != nil {
		return fmt.Errorf("cannot fetch the current controller deployment: %w", err)
	}

	if err := selfmonitoringapiaccess.DisableSelfMonitoringInControllerDeployment(
		updatedDeployment,
		ControllerContainerName,
		true,
	); err != nil {
		return fmt.Errorf("cannot apply settings to disable self-monitoring to the controller deployment: %w", err)
	}

	return r.Client.Update(ctx, updatedDeployment)
}

func (r *OperatorConfigurationReconciler) markAsDegraded(
	ctx context.Context,
	resource *dash0v1alpha1.Dash0OperatorConfiguration,
	reason string,
	message string,
	logger *logr.Logger,
) error {
	resource.EnsureResourceIsMarkedAsDegraded(
		reason,
		message,
	)
	if err := r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, "Failed to update Dash0 operator status conditions, requeuing reconcile request.")
		return err
	}
	return nil
}
