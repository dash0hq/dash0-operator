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
}

type AutoOperatorConfigurationResourceHandler struct {
	client.Client
	OperatorNamespace  string
	NamePrefix         string
	bypassWebhookCheck bool
}

const (
	operatorConfigurationAutoResourceName = "dash0-operator-configuration-auto-resource"
)

func (r *AutoOperatorConfigurationResourceHandler) CreateOrUpdateOperatorConfigurationResource(
	ctx context.Context,
	operatorConfiguration *OperatorConfigurationValues,
	logger *logr.Logger,
) error {
	if err := r.validateOperatorConfiguration(operatorConfiguration); err != nil {
		return err
	}

	go func() {
		// There is a validation webhook for operator configuration resources. Thus, before we can create or update an
		// operator configuration resource, we need to wait for the webhook endpoint to become available.
		if err := r.waitForWebserviceEndpoint(ctx, logger); err != nil {
			logger.Error(err, "failed to create the Dash0 operator configuration resource")
		}

		// Even if we wait for the validation webhook to become available (see above), we sometimes get a couple of
		// retry attempts that fail with
		//   create/update operator configuration resource at startup failed in attempt x/6, will be retried.
		//   [...]
		//   failed calling webhook \"validate-operator-configuration.dash0.com\":
		//   failed to call webhook: Post \"https://dash0-operator-webhook-service.dash0-system.svc:443/v1alpha1/validate/operator-configuration?timeout=5s\":
		//   tls: failed to verify certificate: x509: certificate signed by unknown authority
		//   (possibly because of \"crypto/rsa: verification error\" while trying to verify candidate authority certificate
		//   \"dash0-operator-ca\")"
		//
		// This self-heals after a few attempts. Still, the log entries might be confusing. To lower the probability of
		// this happening, we wait for a few seconds before creating/updating the operator configuration resource.
		if !r.bypassWebhookCheck {
			time.Sleep(10 * time.Second)
		}

		if err := r.createOrUpdateOperatorConfigurationResourceWithRetry(ctx, operatorConfiguration, logger); err != nil {
			logger.Error(err, "failed to create the Dash0 operator configuration resource")
		}
	}()
	return nil
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
			Factor:   1.0,
			Steps:    30,
			Cap:      30 * time.Second,
		},
		false,
		logger,
	); err != nil {
		return fmt.Errorf("failed to wait for the webservice endpoint to become available: %w", err)
	}

	return nil
}

func (r *AutoOperatorConfigurationResourceHandler) checkWebServiceEndpoint(
	ctx context.Context,
) error {
	endpoints := corev1.Endpoints{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: r.OperatorNamespace,
		Name:      fmt.Sprintf("%s-webhook-service", r.NamePrefix),
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
	operatorConfiguration *OperatorConfigurationValues,
	logger *logr.Logger,
) error {
	return util.RetryWithCustomBackoff(
		"create/update operator configuration resource at startup",
		func() error {
			return r.createOperatorConfigurationResourceOnce(ctx, operatorConfiguration, logger)
		},
		wait.Backoff{
			Duration: 3 * time.Second,
			Factor:   1.5,
			Steps:    6,
			Cap:      60 * time.Second,
		},
		true,
		logger,
	)
}

func (r *AutoOperatorConfigurationResourceHandler) createOperatorConfigurationResourceOnce(
	ctx context.Context,
	operatorConfiguration *OperatorConfigurationValues,
	logger *logr.Logger,
) error {
	authorization := dash0v1alpha1.Authorization{}
	if operatorConfiguration.Token != "" {
		authorization.Token = &operatorConfiguration.Token
	} else {
		authorization.SecretRef = &dash0v1alpha1.SecretRef{
			Name: operatorConfiguration.SecretRef.Name,
			Key:  operatorConfiguration.SecretRef.Key,
		}
	}

	dash0Export := dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint:      operatorConfiguration.Endpoint,
			Authorization: authorization,
		},
	}
	if operatorConfiguration.ApiEndpoint != "" {
		dash0Export.Dash0.ApiEndpoint = operatorConfiguration.ApiEndpoint
	}
	if operatorConfiguration.Dataset != "" {
		dash0Export.Dash0.Dataset = operatorConfiguration.Dataset
	}
	operatorConfigurationResource := dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorConfigurationAutoResourceName,
			Annotations: map[string]string{
				// For clusters managed by ArgoCD, we need to prevent ArgoCD to prune resources that are not created
				// directly via the Helm chart and that have no owner reference. These are all cluster-scoped resources
				// not created via Helm, like cluster roles & cluster role bindings. See also:
				// * https://github.com/argoproj/argo-cd/issues/4764#issuecomment-722661940 -- this is where they say
				//   that only top level resources are pruned (that is basically the same as resources without an owner
				//   reference).
				// * The docs for preventing this on a resource level are here:
				//   https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#no-prune-resources
				"argocd.argoproj.io/sync-options": "Prune=false",
			},
		},
		Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{
				Enabled: ptr.To(operatorConfiguration.SelfMonitoringEnabled),
			},
			Export: &dash0Export,
			KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(operatorConfiguration.KubernetesInfrastructureMetricsCollectionEnabled),
		},
	}

	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.List(ctx, allOperatorConfigurationResources); err != nil {
		return fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err)
	}
	if len(allOperatorConfigurationResources.Items) >= 1 {
		// The validation webhook for the operator configuration resource guarantees that there is only ever one
		// resource per cluster. Thus, we can arbitrarily update the first item in the list.
		existingOperatorConfigurationResource := allOperatorConfigurationResources.Items[0]
		existingOperatorConfigurationResource.Spec = operatorConfigurationResource.Spec
		if err := r.Update(ctx, &existingOperatorConfigurationResource); err != nil {
			return fmt.Errorf("failed to update the Dash0 operator configuration resource: %w", err)
		}
		logger.Info("the Dash0 operator configuration resource has been updated")
		return nil
	}

	if err := r.Create(ctx, &operatorConfigurationResource); err != nil {
		return fmt.Errorf("failed to create the Dash0 operator configuration resource: %w", err)
	}

	logger.Info("a Dash0 operator configuration resource has been created")
	return nil
}