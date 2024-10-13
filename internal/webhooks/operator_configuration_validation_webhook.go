// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"net/http"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
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

func (h *OperatorConfigurationValidationWebhookHandler) Handle(_ context.Context, request admission.Request) admission.Response {
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if operatorConfigurationResource.Spec.SelfMonitoring.Enabled && operatorConfigurationResource.Spec.Export == nil {
		return admission.Denied(
			"The provided Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for self-" +
				"monitoring telemetry.")

	}
	return admission.Allowed("")
}
