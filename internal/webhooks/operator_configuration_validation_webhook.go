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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const ErrorMessageTelemetryCollectionDisabledViaHelm = "Telemetry collection has been disabled via the Helm chart " +
	"(operator.telemetryCollectionEnabled: false), but the provided Dash0 operator configuration resource has " +
	"telemetryCollection.enabled=true. Telemetry collection cannot be enabled via the operator configuration resource " +
	"when it has been disabled via the Helm chart. Instead, run helm upgrade --install to set " +
	"operator.telemetryCollectionEnabled: true via the Helm chart."

const ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid = "The provided Dash0 operator configuration resource has Prometheus CRD support " +
	"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
	"Please either set telemetryCollection.enabled=true or " +
	"prometheusCrdSupport.enabled=false."

const ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure = "The provided Dash0 operator configuration resource has both insecure and insecureSkipVerify " +
	"explicitly enabled for the GRPC export. This is an invalid combination. " +
	"Please set at most one of these two flags to true."

const ErrorMessageOperatorConfigurationExportAndExportsAreMutuallyExclusive = "The provided Dash0 operator configuration resource has both the " +
	"deprecated `export` and the `exports` field set. These fields are mutually exclusive. Please use only the " +
	"`exports` field and remove the `export` field."

const ErrorMessageOperatorConfigurationMonitoringTemplateWithExports = "The provided Dash0 operator configuration resource has a monitoring template with `exports`. Please use the " +
	"`exports` field in the operator configuration and remove the `exports` from the monitoringTemplate.spec."

const ErrorMessageOperatorConfigurationMonitoringTemplateWithExport = "The provided Dash0 operator configuration resource has a monitoring template with `export`. Please use the " +
	"`exports` field in the operator configuration and remove the `export` from the monitoringTemplate.spec."

type OperatorConfigurationValidationWebhookHandler struct {
	Client                            client.Client
	telemetryCollectionEnabledViaHelm bool
}

func NewOperatorConfigurationValidationWebhookHandler(
	k8sClient client.Client,
	telemetryCollectionEnabledViaHelm bool,
) *OperatorConfigurationValidationWebhookHandler {
	return &OperatorConfigurationValidationWebhookHandler{
		Client:                            k8sClient,
		telemetryCollectionEnabledViaHelm: telemetryCollectionEnabledViaHelm,
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
	logger := logd.FromContext(ctx)
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, operatorConfigurationResource); err != nil {
		logger.Warn("rejecting invalid operator configuration resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	spec := operatorConfigurationResource.Spec

	if !h.telemetryCollectionEnabledViaHelm && spec.TelemetryCollection.Enabled != nil && *spec.TelemetryCollection.Enabled {
		logger.Warn(ErrorMessageTelemetryCollectionDisabledViaHelm)
		return admission.Denied(ErrorMessageTelemetryCollectionDisabledViaHelm)
	}

	// Reject if both the deprecated export and the new exports field are set.
	//nolint:staticcheck
	if spec.Export != nil && len(spec.Exports) > 0 {
		logger.Warn(ErrorMessageOperatorConfigurationExportAndExportsAreMutuallyExclusive)
		return admission.Denied(ErrorMessageOperatorConfigurationExportAndExportsAreMutuallyExclusive)
	}

	//nolint:staticcheck
	if spec.KubernetesInfrastructureMetricsCollectionEnabled != nil &&
		//nolint:staticcheck
		!*spec.KubernetesInfrastructureMetricsCollectionEnabled &&
		// if spec.TelemetryCollection.Enabled=false, we actually set
		// spec.KubernetesInfrastructureMetricsCollectionEnabled = false ourselves in the mutating webhook.
		*spec.TelemetryCollection.Enabled {
		// The deprecated option has been explicitly set to false, log warning to ask users to switch to the new option.
		logger.Warn("The setting Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollectionEnabled is deprecated: Please use Dash0OperatorConfiguration.spec.kubernetesInfrastructureMetricsCollection.enabled instead.")
	}

	if util.ReadBoolPointerWithDefault(spec.SelfMonitoring.Enabled, true) &&
		len(spec.Exports) == 0 {
		msg := "The provided Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
			"export configuration. Either disable self-monitoring or provide an export configuration for self-" +
			"monitoring telemetry."
		logger.Warn(msg)
		return admission.Denied(msg)
	}

	if !util.ReadBoolPointerWithDefault(spec.TelemetryCollection.Enabled, true) {
		if util.ReadBoolPointerWithDefault(spec.KubernetesInfrastructureMetricsCollection.Enabled, true) {
			msg := "The provided Dash0 operator configuration resource has Kubernetes infrastructure metrics collection " +
				"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
				"Please either set telemetryCollection.enabled=true or " +
				"kubernetesInfrastructureMetricsCollection.enabled=false."
			logger.Warn(msg)
			return admission.Denied(msg)
		}
		//nolint:staticcheck
		if util.ReadBoolPointerWithDefault(spec.KubernetesInfrastructureMetricsCollectionEnabled, true) {
			msg := "The provided Dash0 operator configuration resource has Kubernetes infrastructure metrics collection " +
				"explicitly enabled (via the deprecated legacy setting " +
				"kubernetesInfrastructureMetricsCollectionEnabled), although telemetry collection is disabled. " +
				"This is an invalid combination. Please either set telemetryCollection.enabled=true or " +
				"kubernetesInfrastructureMetricsCollection.enabled=false."
			logger.Warn(msg)
			return admission.Denied(msg)
		}
		if util.ReadBoolPointerWithDefault(spec.CollectPodLabelsAndAnnotations.Enabled, true) {
			msg := "The provided Dash0 operator configuration resource has pod label and annotation collection " +
				"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
				"Please either set telemetryCollection.enabled=true or " +
				"collectPodLabelsAndAnnotations.enabled=false."
			logger.Warn(msg)
			return admission.Denied(msg)
		}
		if util.ReadBoolPointerWithDefault(spec.CollectNamespaceLabelsAndAnnotations.Enabled, true) {
			msg := "The provided Dash0 operator configuration resource has namespace label and annotation collection " +
				"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
				"Please either set telemetryCollection.enabled=true or " +
				"collectNamespaceLabelsAndAnnotations.enabled=false."
			logger.Warn(msg)
			return admission.Denied(msg)
		}
		if util.ReadBoolPointerWithDefault(spec.PrometheusCrdSupport.Enabled, true) {
			logger.Warn(ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid)
			return admission.Denied(ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid)
		}
		if spec.Profiling != nil && util.ReadBoolPointerWithDefault(spec.Profiling.Enabled, false) {
			msg := "The provided Dash0 operator configuration resource has profiling " +
				"explicitly enabled, although telemetry collection is disabled. This is an invalid combination. " +
				"Please either set telemetryCollection.enabled=true or " +
				"profiling.enabled=false."
			logger.Warn(msg)
			return admission.Denied(msg)
		}
	}

	for _, export := range spec.Exports {
		if !validateGrpcExportInsecureFlags(&export) {
			logger.Warn(ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure)
			return admission.Denied(ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure)
		}
	}

	if spec.MonitoringTemplate != nil {
		// Defining an export in the monitoring template makes no sense, since all monitoring resources would have their
		// individual export then, but it is the same export target over and over. Users should put the export into the
		// operator configuration instead.

		//nolint:staticcheck
		if spec.MonitoringTemplate.Spec.Export != nil {
			logger.Warn(ErrorMessageOperatorConfigurationMonitoringTemplateWithExport)
			return admission.Denied(ErrorMessageOperatorConfigurationMonitoringTemplateWithExport)
		}
		if len(spec.MonitoringTemplate.Spec.Exports) > 0 {
			logger.Warn(ErrorMessageOperatorConfigurationMonitoringTemplateWithExports)
			return admission.Denied(ErrorMessageOperatorConfigurationMonitoringTemplateWithExports)
		}
	}

	if request.Operation == admissionv1.Create {
		allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
		if err := h.Client.List(ctx, allOperatorConfigurationResources); err != nil {
			wrappedErr := fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err)
			logger.Error(wrappedErr, "failed to list all Dash0 operator configuration resources")
			return admission.Errored(http.StatusInternalServerError, wrappedErr)
		}
		if len(allOperatorConfigurationResources.Items) > 0 {
			msg := fmt.Sprintf("At least one Dash0 operator configuration resource (%s) already exists in this cluster. "+
				"Only one operator configuration resource is allowed per cluster.",
				allOperatorConfigurationResources.Items[0].Name,
			)
			logger.Warn(msg)
			return admission.Denied(msg)
		}
	}

	return admission.Allowed("")
}
