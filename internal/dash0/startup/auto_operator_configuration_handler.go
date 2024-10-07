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
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type SecretRef struct {
	Name string
	Key  string
}

type OperatorConfigurationValues struct {
	Endpoint string
	Token    string
	SecretRef
	ApiEndpoint string
}

type AutoOperatorConfigurationResourceHandler struct {
	client.Client
	OperatorNamespace  string
	NamePrefix         string
	bypassWebhookCheck bool
}

const (
	operatorConfigurationAutoResourceName = "dash0-operator-configuration-auto-resource"

	alreadyExistsMessage = "The operator is configured to deploy an operator configuration resource at startup, but there is already" +
		"an operator configuration resource in the cluster. Hence no action is necessary. (This is not an error.)"
)

func (r *AutoOperatorConfigurationResourceHandler) CreateOperatorConfigurationResource(
	ctx context.Context,
	operatorConfiguration *OperatorConfigurationValues,
	logger *logr.Logger,
) error {

	// Fast path: check early on if there is already an operator configuration resource, skip all other steps if so.
	// We will repeat this check immediately before creating the operator configuration resource, so if the check fails
	// with an error we will ignore that error for now.
	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.List(ctx, allOperatorConfigurationResources); err == nil {
		if len(allOperatorConfigurationResources.Items) >= 1 {
			logger.Info(alreadyExistsMessage)
			return nil
		}
	}

	if err := r.validateOperatorConfiguration(operatorConfiguration); err != nil {
		return err
	}

	go func() {
		// There is a validation webhook for operator configuration resources. Thus, before we can create an operator
		// configuration resource, we need to wait for the webhook endpoint to become available.
		if err := r.waitForWebserviceEndpoint(ctx, logger); err != nil {
			logger.Error(err, "failed to create the Dash0 operator configuration resource")
		}
		if err := r.createOperatorConfigurationResourceWithRetry(ctx, operatorConfiguration, logger); err != nil {
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

func (r *AutoOperatorConfigurationResourceHandler) createOperatorConfigurationResourceWithRetry(
	ctx context.Context,
	operatorConfiguration *OperatorConfigurationValues,
	logger *logr.Logger,
) error {
	return util.RetryWithCustomBackoff(
		"create operator configuration resource at startup",
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
	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.List(ctx, allOperatorConfigurationResources); err != nil {
		return fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err)
	}
	if len(allOperatorConfigurationResources.Items) >= 1 {
		logger.Info(alreadyExistsMessage)
		return nil
	}

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
				Enabled: true,
			},
			Export: &dash0Export,
		},
	}
	if err := r.Create(ctx, &operatorConfigurationResource); err != nil {
		return fmt.Errorf("failed to create the Dash0 operator configuration resource: %w", err)
	}

	logger.Info("a Dash0 operator configuration resource has been created")
	return nil
}
