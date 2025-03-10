// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	Clientset                       *kubernetes.Clientset
	ApiClients                      []ApiClient
	Scheme                          *runtime.Scheme
	Recorder                        record.EventRecorder
	BackendConnectionManager        *backendconnection.BackendConnectionManager
	OperatorDeploymentUID           types.UID
	OTelSdkStarter                  *selfmonitoringapiaccess.OTelSdkStarter
	DanglingEventsTimeouts          *util.DanglingEventsTimeouts
	Images                          util.Images
	OperatorNamespace               string
	SecretRefResolverDeploymentName string
	DevelopmentMode                 bool
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
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
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

	resourceDeleted := false
	checkResourceResult, err := util.VerifyThatResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		&logger,
	)
	if err != nil {
		logger.Error(err, "operator configuration resource existence check failed")
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		resourceDeleted = true
	} else if checkResourceResult.StopReconcile {
		logger.Info("stopping operator configuration resource reconcile request after resource existence check")
		return ctrl.Result{}, nil
	}

	if resourceDeleted {
		logger.Info("Reconciling the deletion of the operator configuration resource", "name", req.Name)
		for _, apiClient := range r.ApiClients {
			apiClient.RemoveApiEndpointAndDataset(ctx, &logger)
			apiClient.RemoveAuthToken(ctx, &logger)
		}
		r.OTelSdkStarter.RemoveOTelSdkParameters(
			ctx,
			&logger,
		)
		r.OTelSdkStarter.RemoveAuthToken(
			ctx,
			&logger,
		)

		if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	operatorConfigurationResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)
	if operatorConfigurationResource.IsDegraded() {
		logger.Info("The operator configuration resource is degraded, stopping reconciliation.")
		return ctrl.Result{}, nil
	}
	stopReconcile, err :=
		util.VerifyThatResourceIsUniqueInScope(
			ctx,
			r.Client,
			req,
			operatorConfigurationResource,
			updateStatusFailedMessageOperatorConfiguration,
			&logger,
		)
	if err != nil {
		logger.Error(err, "error in operator configuration resource uniqueness check")
		return ctrl.Result{}, err
	} else if stopReconcile {
		logger.Info("stopping operator configuration resource reconcile after uniqueness check")
		return ctrl.Result{}, nil
	}

	if _, err = util.InitStatusConditions(
		ctx,
		r.Client,
		operatorConfigurationResource,
		operatorConfigurationResource.Status.Conditions,
		&logger,
	); err != nil {
		logger.Error(err, "error when initializing operator configuration resource status conditions")
		return ctrl.Result{}, err
	}

	if err = selfmonitoringapiaccess.ExchangeSecretRefForTokenIfNecessary(
		ctx,
		r.Client,
		r.OperatorNamespace,
		r.SecretRefResolverDeploymentName,
		operatorConfigurationResource,
		&logger,
	); err != nil {
		logger.Error(err, "cannot exchange secret ref for token")
		return ctrl.Result{}, err
	}

	if selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			operatorConfigurationResource,
			&logger,
		); err != nil {
		logger.Error(
			err,
			"cannot generate self-monitoring configuration from operator configuration resource",
		)
		r.OTelSdkStarter.RemoveOTelSdkParameters(ctx, &logger)
		r.OTelSdkStarter.RemoveAuthToken(ctx, &logger)
		return ctrl.Result{}, err
	} else {
		logger.Info("applying self-monitoring configuration", "configuration", selfMonitoringConfiguration)
		r.applyOperatorManagerSelfMonitoringSettings(
			ctx,
			selfMonitoringConfiguration,
			&logger,
		)
	}

	r.applyApiAccessSettings(ctx, operatorConfigurationResource, logger)

	if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
		return ctrl.Result{}, err
	}

	operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, operatorConfigurationResource); err != nil {
		logger.Error(err, updateStatusFailedMessageOperatorConfiguration)
		return ctrl.Result{}, fmt.Errorf("cannot mark the Dash0 operator configuration resource as available: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *OperatorConfigurationReconciler) applyOperatorManagerSelfMonitoringSettings(
	ctx context.Context,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringConfiguration,
	logger *logr.Logger,
) {
	if selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		r.OTelSdkStarter.SetOTelSdkParameters(
			ctx,
			selfMonitoringAndApiAccessConfiguration.Export,
			r.OperatorDeploymentUID,
			r.Images.GetOperatorVersion(),
			r.DevelopmentMode,
			logger,
		)
	} else {
		r.OTelSdkStarter.RemoveOTelSdkParameters(
			ctx,
			logger,
		)
	}
}

func (r *OperatorConfigurationReconciler) applyApiAccessSettings(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logr.Logger,
) {
	if operatorConfigurationResource.HasDash0ApiAccessConfigured() {
		dataset := operatorConfigurationResource.Spec.Export.Dash0.Dataset
		if dataset == "" {
			dataset = util.DatasetDefault
		}
		for _, apiClient := range r.ApiClients {
			apiClient.SetApiEndpointAndDataset(
				ctx,
				&ApiConfig{
					Endpoint: operatorConfigurationResource.Spec.Export.Dash0.ApiEndpoint,
					Dataset:  dataset,
				}, &logger)

		}
	} else {
		logger.Info("The API endpoint setting required for managing dashboards or check rules via the operator is " +
			"missing or has been removed, the operator will not update dashboards nor check rules in Dash0.")
		for _, apiClient := range r.ApiClients {
			apiClient.RemoveApiEndpointAndDataset(ctx, &logger)
		}
	}

	var authToken *string
	usesSecretRef := false
	if operatorConfigurationResource.Spec.Export != nil &&
		operatorConfigurationResource.Spec.Export.Dash0 != nil {
		authToken = operatorConfigurationResource.Spec.Export.Dash0.Authorization.Token
		usesSecretRef = operatorConfigurationResource.Spec.Export.Dash0.Authorization.SecretRef != nil
	}

	if usesSecretRef {
		for _, apiClient := range r.ApiClients {
			apiClient.RemoveAuthToken(ctx, &logger)
		}
	} else {
		if authToken != nil && *authToken != "" {
			for _, apiClient := range r.ApiClients {
				apiClient.SetAuthToken(ctx, *authToken, &logger)
			}
		} else {
			// If the auth token is provided via a secret ref, we cannot remove it here, as we might accidentally
			// delete a token that has been resolved via the secret ref resolver. But if the operator configuration
			// resource does not have a secret ref, we can and should remove the auth token to keep the API client's
			// state consistent with the operator configuration resource's state.
			logger.Info("The Dash0 auth token required for managing dashboards or check rules via the operator " +
				"is missing or has been removed, the operator will not update dashboards nor check rules in Dash0.")
			for _, apiClient := range r.ApiClients {
				apiClient.RemoveAuthToken(ctx, &logger)
			}
		}
	}
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
