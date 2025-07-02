// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

type OperatorConfigurationMutatingWebhookHandler struct {
	Client client.Client
}

func (h *OperatorConfigurationMutatingWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/mutate/operator-configuration", handler)

	return nil
}

func (h *OperatorConfigurationMutatingWebhookHandler) Handle(_ context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can normalize the resource here and
	// verify that it is valid (after having been normalized) in the validating webhook.
	// Note that default values from // +kubebuilder:default comments from
	// api/dash0monitoring/v1alpha1/operator_configuration_types.go have already been applied by the time this webhook
	// is called.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	patchRequired := h.normalizeOperatorConfigurationResourceSpec(&operatorConfigurationResource.Spec)

	if !patchRequired {
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(operatorConfigurationResource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied operator configuration resource to JSON: %w", err)
		return admission.Errored(http.StatusInternalServerError, wrappedErr)
	}

	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *OperatorConfigurationMutatingWebhookHandler) normalizeOperatorConfigurationResourceSpec(spec *dash0v1alpha1.Dash0OperatorConfigurationSpec) bool {
	patchRequired := false
	if spec.TelemetryCollection.Enabled == nil {
		spec.TelemetryCollection.Enabled = ptr.To(true)
		patchRequired = true
	}
	telemetryCollectionEnabled := *spec.TelemetryCollection.Enabled
	if spec.SelfMonitoring.Enabled == nil {
		spec.SelfMonitoring.Enabled = ptr.To(true)
		patchRequired = true
	}
	if spec.KubernetesInfrastructureMetricsCollection.Enabled == nil {
		spec.KubernetesInfrastructureMetricsCollection.Enabled = ptr.To(telemetryCollectionEnabled)
		patchRequired = true
	}
	//nolint:staticcheck
	if spec.KubernetesInfrastructureMetricsCollectionEnabled == nil {
		//nolint:staticcheck
		spec.KubernetesInfrastructureMetricsCollectionEnabled = ptr.To(telemetryCollectionEnabled)
		patchRequired = true
	}
	if spec.CollectPodLabelsAndAnnotations.Enabled == nil {
		spec.CollectPodLabelsAndAnnotations.Enabled = ptr.To(telemetryCollectionEnabled)
		patchRequired = true
	}
	return patchRequired
}
