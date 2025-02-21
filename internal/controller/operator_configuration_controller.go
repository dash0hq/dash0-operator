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
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OperatorConfigurationReconciler struct {
	client.Client
	Clientset                        *kubernetes.Clientset
	ApiClients                       []ApiClient
	Scheme                           *runtime.Scheme
	Recorder                         record.EventRecorder
	BackendConnectionManager         *backendconnection.BackendConnectionManager
	OperatorDeploymentSelfReference  *appsv1.Deployment // TODO should not be required anymore, remove?
	OTelSdkStarter                   *selfmonitoringapiaccess.OTelSdkStarter
	DanglingEventsTimeouts           *util.DanglingEventsTimeouts
	Images                           util.Images
	OperatorNamespace                string
	SecretRefSatelliteDeploymentName string
	DevelopmentMode                  bool
}

const (
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
	logger.Info("processing reconcile request for an operator configuration resource")

	var resource *dash0v1alpha1.Dash0OperatorConfiguration
	resourceDeleted := false
	checkResourceResult, err := util.VerifyThatResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		&logger,
	)
	if err != nil {
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		resourceDeleted = true
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	if resourceDeleted {
		logger.Info("Reconciling the deletion of the operator configuration resource", "name", req.Name)
		for _, apiClient := range r.ApiClients {
			apiClient.RemoveApiEndpointAndDataset()
		}
		// TODO This will trigger a restart of the manager pod, which in turn will trigger the recreation of the
		// auto operator configuration resource if this has been enabled via Helm, although the user has just deleted
		// that resource. The solution for this problem is to avoid the manager pod restart for self-monitoring/API
		// access purposes, see https://linear.app/dash0/issue/ENG-3455/avoid-controller-pod-self-restart-for-self-monitoringapi-auth-token
		if err = r.removeSelfMonitoringAndApiAccessAndUpdate(ctx); err != nil {
			logger.Error(err, "cannot disable self-monitoring/API access of the operator manager deployment, requeuing reconcile request.")
			return ctrl.Result{
				Requeue: true,
			}, nil
		} else {

			logger.Info("Self-monitoring of the operator manager deployment has been disabled")
		}
		if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	resource = checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)
	if resource.IsDegraded() {
		logger.Info("The operator configuration resource is degraded, stopping reconciliation.")
		return ctrl.Result{}, nil
	}
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
		return ctrl.Result{}, err
	} else if stopReconcile {
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
			dataset = util.DatasetDefault
		}
		for _, apiClient := range r.ApiClients {
			apiClient.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: resource.Spec.Export.Dash0.ApiEndpoint,
				Dataset:  dataset,
			}, &logger)
		}
	} else {
		logger.Info("Settings required for managing dashboards or check rules via the operator are missing, the " +
			"operator will not update dashboards nor check rules in Dash0.")
		for _, apiClient := range r.ApiClients {
			apiClient.RemoveApiEndpointAndDataset()
		}
	}

	logger.Info("XXX operator configuration reconcile", "operator configuration resource", resource)
	// TODO the way we retrieve the "current config" is now broken, needs to be fixed (fetch from otelsdkstarter and
	// also from where ever we end up storing the API access config)
	currentSelfMonitoringAndApiAccessConfiguration, err :=
		selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromOperatorManagerDeployment(
			r.OperatorDeploymentSelfReference,
		)
	if err != nil {
		logger.Error(err, "cannot get self-monitoring/API access configuration from operator manager deployment")
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

	logger.Info(
		"XXX calling exchangeSecretRefForTokenIfNecessary",
		"newSelfMonitoringAndApiAccessConfiguration",
		newSelfMonitoringAndApiAccessConfiguration,
	)
	if err = selfmonitoringapiaccess.ExchangeSecretRefForTokenIfNecessary(
		ctx,
		r.Client,
		r.OperatorNamespace,
		r.SecretRefSatelliteDeploymentName,
		newSelfMonitoringAndApiAccessConfiguration,
		resource,
		&logger,
	); err != nil {
		logger.Error(err, "cannot exchange secret ref for token")
		return ctrl.Result{}, err
	}

	logger.Info("XXX previous self-monitoring/API access config", "config", currentSelfMonitoringAndApiAccessConfiguration)
	logger.Info("XXX new      self-monitoring/API access config", "config", newSelfMonitoringAndApiAccessConfiguration)
	if !reflect.DeepEqual(currentSelfMonitoringAndApiAccessConfiguration, newSelfMonitoringAndApiAccessConfiguration) {
		// 1. operator_configuration_controller#Reconcile -> operator_configuration_controller#applySelfMonitoringAndApiAccess
		logger.Info("Applying the new self-monitoring and API access configuration.")
		if err = r.applySelfMonitoringAndApiAccess(
			newSelfMonitoringAndApiAccessConfiguration,
			&logger,
		); err != nil {
			// TODO error message and status needs update
			logger.Error(err, "cannot apply self-monitoring configuration to the operator manager deployment")
			if statusUpdateErr := util.MarkOperatorConfigurationAsDegradedAndUpdateStatus(
				ctx,
				r.Status(),
				resource,
				"CannotUpdatedOperatorManagerDeployment",
				"Could not update the operator manager deployment to reflect the self-monitoring settings.",
				&logger,
			); statusUpdateErr != nil {
				return ctrl.Result{}, statusUpdateErr
			}
			return ctrl.Result{}, err
		}

	} else {
		logger.Info("The self-monitoring and API access configuration is up to date.")
	}

	if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
		return ctrl.Result{}, err
	}

	resource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, updateStatusFailedMessageOperatorConfiguration)
		return ctrl.Result{}, fmt.Errorf("cannot mark the Dash0 operator configuration resource as available: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *OperatorConfigurationReconciler) applySelfMonitoringAndApiAccess(
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	logger *logr.Logger,
) error {
	if selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		// 2. operator_configuration_controller#applySelfMonitoringAndApiAccess -> selfmonitoringapiaccess.EnableSelfMonitoringInOperatorManagerDeployment
		if err := selfmonitoringapiaccess.EnableSelfMonitoringInOperatorManagerDeployment(
			r.OTelSdkStarter,
			selfMonitoringAndApiAccessConfiguration,
			r.OperatorDeploymentSelfReference.UID,
			r.Images.GetOperatorVersion(),
			r.DevelopmentMode,
			logger,
		); err != nil {
			return fmt.Errorf("cannot add the Dash0 API token to the operator manager deployment: %w", err)
		}
		// TODO bring back the cases below later
		//} else if selfMonitoringAndApiAccessConfiguration.HasDash0ApiAccessConfigured() {
		//	// TODO resolve secret ref (potentially) and get the API token
		//	if err := selfmonitoringapiaccess.UpdateApiTokenWithoutAddingSelfMonitoringToOperatorManagerDeployment(
		//		operatorManagerDeployment,
		//		OperatorManagerContainerName,
		//		selfMonitoringAndApiAccessConfiguration.GetDash0Authorization(),
		//	); err != nil {
		//		return fmt.Errorf("cannot add the Dash0 API token to the operator manager deployment: %w", err)
		//	}
		//	// TODO remove self-monitoring OTel SDK configuration, stop OTel SDK
		//	if err := selfmonitoringapiaccess.DisableSelfMonitoringInOperatorManagerDeployment(
		//		operatorManagerDeployment,
		//		OperatorManagerContainerName,
		//		false,
		//	); err != nil {
		//		return fmt.Errorf("cannot apply settings to disable self-monitoring to the operator manager deployment: %w", err)
		//	}
		//} else {
		//	// TODO remove self-monitoring OTel SDK configuration, stop OTel SDK
		//	if err := selfmonitoringapiaccess.DisableSelfMonitoringInOperatorManagerDeployment(
		//		operatorManagerDeployment,
		//		OperatorManagerContainerName,
		//		true,
		//	); err != nil {
		//		return fmt.Errorf("cannot apply settings to the operator manager deployment to disable self-monitoring and API access: %w", err)
		//	}
	}
	return nil
}

func (r *OperatorConfigurationReconciler) removeSelfMonitoringAndApiAccessAndUpdate(ctx context.Context) error {
	// TODO needs to be updated
	operatorManagerDeployment := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(r.OperatorDeploymentSelfReference), operatorManagerDeployment); err != nil {
		return fmt.Errorf("cannot fetch the current operator manager deployment: %w", err)
	}

	if err := selfmonitoringapiaccess.DisableSelfMonitoringInOperatorManagerDeployment(
		operatorManagerDeployment,
		true,
	); err != nil {
		return fmt.Errorf("cannot apply settings to disable self-monitoring to the operator manager deployment: %w", err)
	}

	return r.Client.Update(ctx, operatorManagerDeployment, &client.UpdateOptions{FieldManager: util.FieldManager})
}

func (r *OperatorConfigurationReconciler) reconcileOpenTelemetryCollector(
	ctx context.Context,
	logger *logr.Logger,
) error {
	if err, _ := r.BackendConnectionManager.ReconcileOpenTelemetryCollector(
		ctx,
		r.Images,
		r.OperatorNamespace,
		nil,
		backendconnection.TriggeredByDash0ResourceReconcile,
	); err != nil {
		logger.Error(err, "Failed to reconcile the OpenTelemetry collector, requeuing reconcile request.")
		return err
	}
	return nil
}
