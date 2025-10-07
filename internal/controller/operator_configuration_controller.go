// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OperatorConfigurationReconciler struct {
	client.Client
	clientset                   *kubernetes.Clientset
	apiClients                  []ApiClient
	authTokenClients            []selfmonitoringapiaccess.AuthTokenClient
	collectorManager            *collectors.CollectorManager
	pseudoClusterUid            types.UID
	operatorDeploymentNamespace string
	operatorDeploymentUID       types.UID
	operatorDeploymentName      string
	OperatorManagerPodName      string
	oTelSdkStarter              *selfmonitoringapiaccess.OTelSdkStarter
	DanglingEventsTimeouts      *util.DanglingEventsTimeouts
	images                      util.Images
	operatorNamespace           string
	developmentMode             bool
}

const (
	updateStatusFailedMessageOperatorConfiguration = "Failed to update Dash0 operator configuration status " +
		"conditions, requeuing reconcile request."
)

var (
	operatorReconcileRequestMetric otelmetric.Int64Counter
)

func NewOperatorConfigurationReconciler(
	k8sClient client.Client,
	clientset *kubernetes.Clientset,
	apiClients []ApiClient,
	collectorManager *collectors.CollectorManager,
	pseudoClusterUid types.UID,
	operatorDeploymentNamespace string,
	operatorDeploymentUID types.UID,
	operatorDeploymentName string,
	oTelSdkStarter *selfmonitoringapiaccess.OTelSdkStarter,
	images util.Images,
	operatorNamespace string,
	developmentMode bool,
) *OperatorConfigurationReconciler {
	return &OperatorConfigurationReconciler{
		Client:                      k8sClient,
		clientset:                   clientset,
		apiClients:                  apiClients,
		collectorManager:            collectorManager,
		pseudoClusterUid:            pseudoClusterUid,
		operatorDeploymentNamespace: operatorDeploymentNamespace,
		operatorDeploymentUID:       operatorDeploymentUID,
		operatorDeploymentName:      operatorDeploymentName,
		oTelSdkStarter:              oTelSdkStarter,
		images:                      images,
		operatorNamespace:           operatorNamespace,
		developmentMode:             developmentMode,
	}
}

func (r *OperatorConfigurationReconciler) SetAuthTokenClients(authTokenClients []selfmonitoringapiaccess.AuthTokenClient) {
	r.authTokenClients = authTokenClients
}

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
	checkResourceResult, err := resources.VerifyThatResourceExists(
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
		r.removeAllOperatorConfigurationSettings(ctx, logger)
		if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	operatorConfigurationResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)

	stopReconcile, err :=
		resources.VerifyThatResourceIsUniqueInScope(
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

	if _, err = resources.InitStatusConditions(
		ctx,
		r.Client,
		operatorConfigurationResource,
		operatorConfigurationResource.Status.Conditions,
		&logger,
	); err != nil {
		logger.Error(err, "error when initializing operator configuration resource status conditions")
		return ctrl.Result{}, err
	}

	err = r.handleDash0Authorization(ctx, operatorConfigurationResource, &logger)
	if err != nil {
		logger.Error(err, "error when handling the Dash0 authorization information in the operator configuration resource")
		return ctrl.Result{}, err
	}

	selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			operatorConfigurationResource,
			&logger,
		)
	if err != nil {
		logger.Error(
			err,
			"cannot generate self-monitoring configuration from operator configuration resource",
		)
		r.oTelSdkStarter.RemoveOTelSdkParameters(ctx, &logger)
		return ctrl.Result{}, err
	}

	logger.Info("applying self-monitoring configuration", "configuration", selfMonitoringConfiguration)
	r.applyOperatorManagerSelfMonitoringSettings(
		ctx,
		selfMonitoringConfiguration,
		operatorConfigurationResource.Spec.ClusterName,
		&logger,
	)

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

func (r *OperatorConfigurationReconciler) handleDash0Authorization(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) error {
	if operatorConfigurationResource.Spec.Export != nil &&
		operatorConfigurationResource.Spec.Export.Dash0 != nil {
		if operatorConfigurationResource.Spec.Export.Dash0.Authorization.SecretRef != nil {
			// The operator configuration resource uses a secret ref to provide the Dash0 auth token, exchange the secret
			// ref for an actual token and distribute the token value to all clients that need an auth token.
			if err := selfmonitoringapiaccess.ExchangeSecretRefForToken(
				ctx,
				r.Client,
				r.authTokenClients,
				r.operatorNamespace,
				operatorConfigurationResource,
				logger,
			); err != nil {
				logger.Error(err, "cannot exchange secret ref for token")
				return err
			}
		} else if operatorConfigurationResource.Spec.Export.Dash0.Authorization.Token != nil &&
			*operatorConfigurationResource.Spec.Export.Dash0.Authorization.Token != "" {
			// The operator configuration resource uses a token literal to provide the Dash0 auth token, distribute the
			// token value to all clients that need an auth token.
			for _, authTokenClient := range r.authTokenClients {
				authTokenClient.SetAuthToken(ctx, *operatorConfigurationResource.Spec.Export.Dash0.Authorization.Token, logger)
			}
		} else {
			// The operator configuration resource neither has a secret ref nor a token literal, remove the auth token
			// from all clients.
			for _, authTokenClient := range r.authTokenClients {
				authTokenClient.RemoveAuthToken(ctx, logger)
			}
		}
	} else {
		// The operator configuration resource has no Dash0 export and hence also no auth token, remove the auth token
		// from all clients.
		for _, authTokenClient := range r.authTokenClients {
			authTokenClient.RemoveAuthToken(ctx, logger)
		}
	}
	return nil
}

func (r *OperatorConfigurationReconciler) applyOperatorManagerSelfMonitoringSettings(
	ctx context.Context,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringConfiguration,
	clusterName string,
	logger *logr.Logger,
) {
	if selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		r.oTelSdkStarter.SetOTelSdkParameters(
			ctx,
			selfMonitoringAndApiAccessConfiguration.Export,
			r.pseudoClusterUid,
			clusterName,
			r.operatorDeploymentNamespace,
			r.operatorDeploymentUID,
			r.operatorDeploymentName,
			r.images.GetOperatorVersion(),
			r.developmentMode,
			logger,
		)
	} else {
		r.oTelSdkStarter.RemoveOTelSdkParameters(
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
		for _, apiClient := range r.apiClients {
			apiClient.SetApiEndpointAndDataset(
				ctx,
				&ApiConfig{
					Endpoint: operatorConfigurationResource.Spec.Export.Dash0.ApiEndpoint,
					Dataset:  dataset,
				}, &logger)

		}
	} else {
		logger.Info("The API endpoint setting required for managing dashboards, check rules, synthetic checks and	 " +
			"views via the operator is missing or has been removed, the operator will not update these resources " +
			"in Dash0.")
		for _, apiClient := range r.apiClients {
			apiClient.RemoveApiEndpointAndDataset(ctx, &logger)
		}
	}
}

func (r *OperatorConfigurationReconciler) removeAllOperatorConfigurationSettings(ctx context.Context, logger logr.Logger) {
	for _, apiClient := range r.apiClients {
		apiClient.RemoveApiEndpointAndDataset(ctx, &logger)
	}
	r.oTelSdkStarter.RemoveOTelSdkParameters(
		ctx,
		&logger,
	)
	for _, authTokenClient := range r.authTokenClients {
		authTokenClient.RemoveAuthToken(ctx, &logger)
	}
}

func (r *OperatorConfigurationReconciler) reconcileOpenTelemetryCollector(
	ctx context.Context,
	logger *logr.Logger,
) error {
	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(
		ctx,
		nil,
		collectors.TriggeredByDash0ResourceReconcile,
	); err != nil {
		logger.Error(err, "Failed to reconcile the OpenTelemetry collector, requeuing reconcile request.")
		return err
	}
	return nil
}
