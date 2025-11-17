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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid = "The provided Dash0 operator configuration resource has Prometheus CRD support " +
	"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
	"Please either set telemetryCollection.enabled=true or " +
	"prometheusCrdSupport.enabled=false."

const ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure = "The provided Dash0 operator configuration resource has both insecure and insecureSkipVerify " +
	"explicitly enabled for the GRPC export. This is an invalid combination. " +
	"Please set at most one of these two flags to true."

type OperatorConfigurationValidationWebhookHandler struct {
	Client client.Client
}

func NewOperatorConfigurationValidationWebhookHandler(
	k8sClient client.Client,
) *OperatorConfigurationValidationWebhookHandler {
	return &OperatorConfigurationValidationWebhookHandler{
		Client: k8sClient,
	}
}

func (h *OperatorConfigurationValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/operator-configuration/validate", handler)

	return nil
}

func (h *OperatorConfigurationValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can assume the resource has already
	// been normalized by the mutating webhook.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	logger := log.FromContext(ctx)
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	spec := operatorConfigurationResource.Spec

	//nolint:staticcheck
	if spec.KubernetesInfrastructureMetricsCollectionEnabled != nil &&
		//nolint:staticcheck
		!*spec.KubernetesInfrastructureMetricsCollectionEnabled &&
		// if spec.TelemetryCollection.Enabled=false, we actually set
		// spec.KubernetesInfrastructureMetricsCollectionEnabled = false ourselves in the mutating webhook.
		*spec.TelemetryCollection.Enabled {
		// The deprecated option has been explicitly set to false, log warning to ask users to switch to the new option.
		logger.Info("Warning: The setting Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollectionEnabled is deprecated: Please use Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollection.enabled instead.")
	}

	if util.ReadBoolPointerWithDefault(spec.SelfMonitoring.Enabled, true) &&
		spec.Export == nil {
		return admission.Denied(
			"The provided Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for self-" +
				"monitoring telemetry.")

	}

	if !util.ReadBoolPointerWithDefault(spec.TelemetryCollection.Enabled, true) {
		if util.ReadBoolPointerWithDefault(spec.KubernetesInfrastructureMetricsCollection.Enabled, true) {
			return admission.Denied(
				"The provided Dash0 operator configuration resource has Kubernetes infrastructure metrics collection " +
					"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
					"Please either set telemetryCollection.enabled=true or " +
					"kubernetesInfrastructureMetricsCollection.enabled=false.")
		}
		//nolint:staticcheck
		if util.ReadBoolPointerWithDefault(spec.KubernetesInfrastructureMetricsCollectionEnabled, true) {
			return admission.Denied(
				"The provided Dash0 operator configuration resource has Kubernetes infrastructure metrics collection " +
					"explicitly enabled (via the deprecated legacy setting " +
					"kubernetesInfrastructureMetricsCollectionEnabled), although telemetry collection is disabled. " +
					"This is an invalid combination. Please either set telemetryCollection.enabled=true or " +
					"kubernetesInfrastructureMetricsCollection.enabled=false.")
		}
		if util.ReadBoolPointerWithDefault(spec.CollectPodLabelsAndAnnotations.Enabled, true) {
			return admission.Denied(
				"The provided Dash0 operator configuration resource has pod label and annotation collection " +
					"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
					"Please either set telemetryCollection.enabled=true or " +
					"collectPodLabelsAndAnnotations.enabled=false.")
		}
		if util.ReadBoolPointerWithDefault(spec.PrometheusCrdSupport.Enabled, true) {
			return admission.Denied(ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid)
		}
	}

	if !validateGrpcExportInsecureFlags(spec.Export) {
		return admission.Denied(ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure)
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
