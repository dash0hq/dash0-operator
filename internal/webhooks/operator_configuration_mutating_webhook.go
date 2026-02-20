// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

type OperatorConfigurationMutatingWebhookHandler struct {
	Client client.Client
}

func NewOperatorConfigurationMutatingWebhookHandler(
	k8sClient client.Client,
) *OperatorConfigurationMutatingWebhookHandler {
	return &OperatorConfigurationMutatingWebhookHandler{
		Client: k8sClient,
	}
}

func (h *OperatorConfigurationMutatingWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/operator-configuration/mutate", handler)

	return nil
}

func (h *OperatorConfigurationMutatingWebhookHandler) Handle(_ context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can normalize the resource here and
	// verify that it is valid (after having been normalized) in the validating webhook.
	// Note that default values from // +kubebuilder:default comments from
	// api/operator/v1alpha1/operator_configuration_types.go have already been applied by the time this webhook
	// is called.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	patchRequired, errorResponse := h.normalizeOperatorConfigurationResourceSpec(request, &operatorConfigurationResource.Spec)
	if errorResponse != nil {
		return *errorResponse
	}

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

func (h *OperatorConfigurationMutatingWebhookHandler) normalizeOperatorConfigurationResourceSpec(
	request admission.Request,
	spec *dash0v1alpha1.Dash0OperatorConfigurationSpec,
) (bool, *admission.Response) {
	patchRequired := false

	// Migrate deprecated export field to exports, or strip it if exports is already set.
	// - If only export is set: migrate export → exports (the common upgrade path).
	// - If both are set on UPDATE and exports matches the stored resource: kubectl's three-way merge carried over the
	//   old exports from the stored resource. The user's real change is in export, so we migrate export → exports.
	// - If both are set and exports differs from the stored resource (or this is a CREATE): the user intentionally
	//   changed exports directly (or is actively migrating). We honor exports and discard export.
	//nolint:staticcheck
	if spec.Export != nil {
		if len(spec.Exports) == 0 {
			//nolint:staticcheck
			spec.Exports = []dash0common.Export{*spec.Export}
		} else {
			unchanged, errorResponse := isExportsUnchangedFromOldOperatorConfigurationResource(request, spec.Exports)
			if errorResponse != nil {
				return false, errorResponse
			}
			if unchanged {
				//nolint:staticcheck
				spec.Exports = []dash0common.Export{*spec.Export}
			}
		}
		//nolint:staticcheck
		spec.Export = nil
		patchRequired = true
	}

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
	if spec.CollectNamespaceLabelsAndAnnotations.Enabled == nil {
		// collecting namespace labels and annotations is opt-in, so it defaults to false if unset
		spec.CollectNamespaceLabelsAndAnnotations.Enabled = ptr.To(false)
		patchRequired = true
	}
	if spec.PrometheusCrdSupport.Enabled == nil {
		spec.PrometheusCrdSupport.Enabled = ptr.To(false)
		patchRequired = true
	}
	return patchRequired, nil
}

// isExportsUnchangedFromOldOperatorConfigurationResource checks whether the incoming exports slice matches the
// previously stored exports on an UPDATE operation. This detects when kubectl's three-way merge carried over the old
// exports field (set by a prior webhook migration), so the user's real intent is in the deprecated export field.
// Returns false for CREATE operations or when exports differ. Returns an error response if the OldObject cannot be
// decoded.
func isExportsUnchangedFromOldOperatorConfigurationResource(
	request admission.Request,
	incomingExports []dash0common.Export,
) (bool, *admission.Response) {
	if request.Operation != admissionv1.Update {
		return false, nil
	}
	if request.OldObject.Raw == nil {
		return false, nil
	}
	oldResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.OldObject.Raw, nil, oldResource); err != nil {
		errResponse := admission.Errored(
			http.StatusBadRequest,
			fmt.Errorf("could not decode OldObject for export migration: %w", err),
		)
		return false, &errResponse
	}
	return reflect.DeepEqual(incomingExports, oldResource.Spec.Exports), nil
}
