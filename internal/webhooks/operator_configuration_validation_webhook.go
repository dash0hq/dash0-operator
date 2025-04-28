// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	loggg "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OperatorConfigurationValidationWebhookHandler struct {
	Client client.Client
}

func (h *OperatorConfigurationValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/validate/operator-configuration", handler)

	return nil
}

func (h *OperatorConfigurationValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := loggg.FromContext(ctx)
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	//nolint:staticcheck
	if operatorConfigurationResource.Spec.KubernetesInfrastructureMetricsCollectionEnabled != nil &&
		//nolint:staticcheck
		!*operatorConfigurationResource.Spec.KubernetesInfrastructureMetricsCollectionEnabled {
		// The deprecated option has been explicitly set to false, log warning to ask users to switch to the new option.
		logger.Info("Warning: The setting Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollectionEnabled is deprecated: Please use Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollection.enabled instead.")
	}

	if util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.SelfMonitoring.Enabled, true) &&
		operatorConfigurationResource.Spec.Export == nil {
		return admission.Denied(
			"The provided Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for self-" +
				"monitoring telemetry.")

	}

	if request.Operation == admissionv1.Create {
		allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
		if err := h.Client.List(ctx, allOperatorConfigurationResources); err != nil {
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err))
		}
		if len(allOperatorConfigurationResources.Items) > 0 {
			return admission.Denied(
				fmt.Sprintf("At least one Dash0 operator configuration resource (%s) already exists in this cluster. "+
					"Only one operator configuration resource is allowed per cluster.",
					allOperatorConfigurationResources.Items[0].Name,
				))
		}
	}

	return admission.Allowed("")
}
