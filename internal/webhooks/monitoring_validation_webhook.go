// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	loggg "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

type MonitoringValidationWebhookHandler struct {
	Client client.Client
}

func (h *MonitoringValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/validate/monitoring", handler)

	return nil
}

func (h *MonitoringValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := loggg.FromContext(ctx)

	monitoringResource := &dash0v1alpha1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, monitoringResource); err != nil {
		logger.Info("rejecting invalid monitoring resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	//nolint:staticcheck
	if monitoringResource.Spec.PrometheusScrapingEnabled != nil &&
		//nolint:staticcheck
		!*monitoringResource.Spec.PrometheusScrapingEnabled {
		// The deprecated option has been explicitly set to false, log warning to ask users to switch to the new option.
		logger.Info("Warning: The setting Dash0Monitoring.spec.prometheusScrapingEnabled is deprecated: Please use Dash0Monitoring.spec.prometheusScraping.enabled instead.")
	}

	if monitoringResource.Spec.Export != nil {
		return admission.Allowed("")
	}

	operatorConfigurationList := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := h.Client.List(ctx, operatorConfigurationList, &client.ListOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return admission.Denied("The provided Dash0 monitoring resource does not have an export configuration, and no Dash0 operator configuration resource exists.")
		} else {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	var availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration
	for _, operatorConfiguration := range operatorConfigurationList.Items {
		if operatorConfiguration.IsAvailable() {
			availableOperatorConfigurations = append(availableOperatorConfigurations, operatorConfiguration)
		}
	}

	if len(availableOperatorConfigurations) == 0 {
		return admission.Denied(
			"The provided Dash0 monitoring resource does not have an export configuration, and no Dash0 operator " +
				"configuration resources are available.")
	}
	if len(availableOperatorConfigurations) > 1 {
		return admission.Denied(
			"The provided Dash0 monitoring resource does not have an export configuration, and there is more than " +
				"one available Dash0 operator configuration, remove all but one Dash0 operator configuration resource.")
	}

	opperatorConfiguration := availableOperatorConfigurations[0]

	if opperatorConfiguration.Spec.Export == nil {
		return admission.Denied(
			"The provided Dash0 monitoring resource does not have an export configuration, and the existing Dash0 " +
				"operator configuration does not have an export configuration either.")
	}

	return admission.Allowed("")
}
