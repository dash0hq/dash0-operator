// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	"github.com/dash0hq/dash0-operator/internal/util"
)

type SecretRef struct {
	Name string
	Key  string
}

type OperatorConfigurationValues struct {
	Endpoint string
	Token    string
	SecretRef
	ApiEndpoint                                      string
	Dataset                                          string
	SelfMonitoringEnabled                            bool
	KubernetesInfrastructureMetricsCollectionEnabled bool
	CollectPodLabelsAndAnnotationsEnabled            bool
	ClusterName                                      string
}

type AutoOperatorConfigurationResourceHandler struct {
	client.Client
	OperatorNamespace  string
	WebhookServiceName string
	bypassWebhookCheck bool
}

const (
	operatorConfigurationAutoResourceName = "dash0-operator-configuration-auto-resource"
)

// CreateOrUpdateOperatorConfigurationResource creates or updates the Dash0 operator configuration resource. The
// function will create/update the resource asynchronously, that is, when the function returns the resource might not
// have been created/updated yet. The function will optimistically return the resource that is going to be
// created/updated, without guarantees that the resource will be created/updated successfully.
func (r *AutoOperatorConfigurationResourceHandler) CreateOrUpdateOperatorConfigurationResource(
	ctx context.Context,
	operatorConfigurationValues *OperatorConfigurationValues,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	logger.Info("creating/updating the Dash0 operator configuration resource")
	if err := r.validateOperatorConfiguration(operatorConfigurationValues); err != nil {
		return nil, err
	}

	operatorConfigurationResource := convertValuesToResource(operatorConfigurationValues)
	go func() {
		// There is a validation webhook for operator configuration resources. Thus, before we can create or update an
		// operator configuration resource, we need to wait for the webhook endpoint to become available.
		logger.Info(
			fmt.Sprintf(
				"waiting for the service %s to become available before creating or updating the Dash0 "+
					"operator configuration resource",
				r.WebhookServiceName),
		)

		if err := r.waitForWebserviceEndpoint(ctx, logger); err != nil {
			logger.Error(err, "failed to create the Dash0 operator configuration resource")
			return
		}
		logger.Info(
			fmt.Sprintf("the service %s is available now", r.WebhookServiceName),
		)

		if err := r.createOrUpdateOperatorConfigurationResourceWithRetry(ctx, operatorConfigurationResource, logger); err != nil {
			logger.Error(err, "failed to create the Dash0 operator configuration resource")
			return
		}
	}()

	// optimistically return the resource that we are going to create on the validation webhook becomes available
	return operatorConfigurationResource, nil
}

func (r *AutoOperatorConfigurationResourceHandler) validateOperatorConfiguration(
	operatorConfiguration *OperatorConfigurationValues,
) error {
	if operatorConfiguration.Endpoint == "" {
		return fmt.Errorf("invalid operator configuration: --operator-configuration-endpoint has not been provided")
	}
	if operatorConfiguration.Token == "" {
		if operatorConfiguration.SecretRef.Name == "" {
			return fmt.Errorf("invalid operator configuration: --operator-configuration-endpoint has been provided, " +
				"indicating that an operator configuration resource should be created, but neither " +
				"--operator-configuration-token nor --operator-configuration-secret-ref-name have been provided")
		}
		if operatorConfiguration.SecretRef.Key == "" {
			return fmt.Errorf("invalid operator configuration: --operator-configuration-endpoint has been provided, " +
				"indicating that an operator configuration resource should be created, but neither " +
				"--operator-configuration-token nor --operator-configuration-secret-ref-key have been provided")
		}
	}
	return nil
}

func (r *AutoOperatorConfigurationResourceHandler) waitForWebserviceEndpoint(
	ctx context.Context,
	logger *logr.Logger,
) error {
	if r.bypassWebhookCheck {
		return nil
	}
	if err := util.RetryWithCustomBackoff(
		"waiting for webservice endpoint to become available",
		func() error {
			return r.checkWebServiceEndpoint(ctx)
		},
		wait.Backoff{
			Duration: 1 * time.Second,
			Factor:   1,
			Steps:    60,
		},
		false,
		logger,
	); err != nil {
		return fmt.Errorf("failed to wait for the webservice endpoint to become available: %w", err)
	}

	return nil
}

func (r *AutoOperatorConfigurationResourceHandler) checkWebServiceEndpoint(ctx context.Context) error {
	endpoints := corev1.Endpoints{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: r.OperatorNamespace,
		Name:      r.WebhookServiceName,
	}, &endpoints); err != nil {
		return err
	}

	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) == 0 {
			// wait for the address to be listed in subset.Addresses instead of subset.NotReadyAddresses
			continue
		}
		for _, port := range subset.Ports {
			if port.Port == 9443 {
				return nil
			}
		}
	}

	return fmt.Errorf("the webservice endpoint is not available yet")
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceWithRetry(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) error {
	return util.RetryWithCustomBackoff(
		"create/update operator configuration resource at startup",
		func() error {
			return r.createOrUpdateOperatorConfigurationResourceOnce(ctx, operatorConfigurationResource, logger)
		},
		wait.Backoff{
			Duration: 3 * time.Second,
			Factor:   1.5,
			Steps:    6,
		},
		true,
		logger,
	)
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceOnce(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) error {
	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.List(ctx, allOperatorConfigurationResources); err != nil {
		return fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err)
	}

	if len(allOperatorConfigurationResources.Items) >= 1 {
		// The validation webhook for the operator configuration resource guarantees that there is only ever one
		// resource per cluster. Thus, we can arbitrarily update the first item in the list.
		existingOperatorConfigurationResource := allOperatorConfigurationResources.Items[0]
		// If this is a manually created operator configuration resource, we refuse to overwrite it.
		if existingOperatorConfigurationResource.Name != operatorConfigurationAutoResourceName {
			return util.NewRetryableErrorWithFlag(fmt.Errorf(
				"The configuration provided via Helm instructs the operator manager to create an operator "+
					"configuration resource at startup, that is, operator.dash0Export.enabled is true and "+
					"operator.dash0Export.endpoint has been provided. But there is already an operator configuration "+
					"resource in the cluster with the name %s that has not been created by the operator "+
					"manager. Replacing a manually created operator configuration resource with values provided via "+
					"Helm is not supported. Please either delete the existing operator configuration resource or "+
					"change the Helm values to not create an operator configuration resource at startup, "+
					"e.g. set operator.dash0Export.enabled to false or remove all operator.dash0Export.* values.",
				existingOperatorConfigurationResource.Name),
				// do not retry
				false,
			)
		}

		existingOperatorConfigurationResource.Spec = operatorConfigurationResource.Spec
		if err := r.Update(ctx, &existingOperatorConfigurationResource); err != nil {
			return fmt.Errorf("failed to update the Dash0 operator configuration resource: %w", err)
		}
		logger.Info("the Dash0 operator configuration resource has been updated")
		return nil
	}

	if err := r.Create(ctx, operatorConfigurationResource); err != nil {
		return fmt.Errorf("failed to create the Dash0 operator configuration resource: %w", err)
	}

	logger.Info("a Dash0 operator configuration resource has been created")
	return nil
}

func convertValuesToResource(operatorConfigurationValues *OperatorConfigurationValues) *dash0v1alpha1.Dash0OperatorConfiguration {
	authorization := dash0v1alpha1.Authorization{}
	if operatorConfigurationValues.Token != "" {
		authorization.Token = &operatorConfigurationValues.Token
	} else {
		authorization.SecretRef = &dash0v1alpha1.SecretRef{
			Name: operatorConfigurationValues.SecretRef.Name,
			Key:  operatorConfigurationValues.SecretRef.Key,
		}
	}

	dash0Export := dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint:      operatorConfigurationValues.Endpoint,
			Authorization: authorization,
		},
	}
	if operatorConfigurationValues.ApiEndpoint != "" {
		dash0Export.Dash0.ApiEndpoint = operatorConfigurationValues.ApiEndpoint
	}
	if operatorConfigurationValues.Dataset != "" {
		dash0Export.Dash0.Dataset = operatorConfigurationValues.Dataset
	}
	operatorConfigurationResource := dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorConfigurationAutoResourceName,
			Annotations: map[string]string{
				// For clusters managed by ArgoCD, we need to prevent ArgoCD to sync or prune resources that are not
				// created directly via the Helm chart and that have no owner reference. These are all cluster-scoped
				// resources not created via Helm, like cluster roles & cluster role bindings, but also the operator
				// configuration resource we create here. See also:
				// * https://github.com/argoproj/argo-cd/issues/4764#issuecomment-722661940 -- this is where they say
				//   that only top level resources are pruned (that is basically the same as resources without an owner
				//   reference).
				// * The docs for preventing this on a resource level are here:
				//   https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#no-prune-resources
				//   https://argo-cd.readthedocs.io/en/stable/user-guide/compare-options/#ignoring-resources-that-are-extraneous
				"argocd.argoproj.io/sync-options":    "Prune=false",
				"argocd.argoproj.io/compare-options": "IgnoreExtraneous",
			},
		},
		Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{
				Enabled: ptr.To(operatorConfigurationValues.SelfMonitoringEnabled),
			},
			Export: &dash0Export,
			KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
				Enabled: ptr.To(operatorConfigurationValues.KubernetesInfrastructureMetricsCollectionEnabled),
			},
			CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
				Enabled: ptr.To(operatorConfigurationValues.CollectPodLabelsAndAnnotationsEnabled),
			},
			ClusterName: operatorConfigurationValues.ClusterName,
		},
	}

	return &operatorConfigurationResource
}
