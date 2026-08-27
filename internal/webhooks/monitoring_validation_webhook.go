// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/multierr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

const ErrorMessageMonitoringGrpcExportInvalidInsecure = "The provided Dash0 monitoring resource has both insecure and insecureSkipVerify " +
	"explicitly enabled for the GRPC export. This is an invalid combination. " +
	"Please set at most one of these two flags to true."

const ErrorMessageMonitoringExportAndExportsAreMutuallyExclusive = "The provided Dash0 monitoring resource has both the " +
	"deprecated `export` and the `exports` field set. These fields are mutually exclusive. Please use only the " +
	"`exports` field and remove the `export` field."

type MonitoringValidationWebhookHandler struct {
	Client            client.Client
	operatorNamespace string
}

func NewMonitoringValidationWebhookHandler(
	k8sClient client.Client,
	operatorNamespace string,
) *MonitoringValidationWebhookHandler {
	return &MonitoringValidationWebhookHandler{
		Client:            k8sClient,
		operatorNamespace: operatorNamespace,
	}
}

var (
	// See https://opentelemetry.io/docs/languages/sdk-configuration/general/#otel_propagators.
	validTraceContextPropagators = []string{
		"tracecontext",
		"baggage",
		"b3",
		"b3multi",
		"jaeger",
		"xray",
		"ottrace",
		"none",
	}
)

func (h *MonitoringValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/monitoring/validate", handler)

	return nil
}

func (h *MonitoringValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can assume the resource has already
	// been normalized by the mutating webhook.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	logger := logd.FromContext(ctx)

	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, monitoringResource); err != nil {
		logger.Warn("rejecting invalid monitoring resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if request.Operation == admissionv1.Update {
		if !slices.Contains(monitoringResource.Finalizers, dash0common.MonitoringFinalizerId) {
			// Always allow requests that remove the finalizer. Otherwise, we might accidentally block removing a manually
			// managed monitoring resource from a namespace that is enabled for auto-namespace monitoring.
			return admission.Allowed("")
		}
	}

	instrumentWorkloadsMode := monitoringResource.Spec.InstrumentWorkloads.Mode
	if slices.Contains(util.RestrictedNamespaces, request.Namespace) && instrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeNone {
		msg := fmt.Sprintf(
			"Rejecting the deployment of Dash0 monitoring resource \"%s\" to the Kubernetes system namespace "+
				"\"%s\" with instrumentWorkloads.mode=%s, use instrumentWorkloads.mode=none instead.",
			request.Name,
			request.Namespace,
			instrumentWorkloadsMode,
		)
		logger.Warn(msg)
		return admission.Denied(msg)
	}
	if request.Namespace == h.operatorNamespace && instrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeNone {
		msg := fmt.Sprintf(
			"Rejecting the deployment of Dash0 monitoring resource \"%s\" to the Dash0 operator namespace "+
				"\"%s\" with instrumentWorkloads.mode=%s, use instrumentWorkloads.mode=none instead.",
			request.Name,
			request.Namespace,
			instrumentWorkloadsMode,
		)
		logger.Warn(msg)
		return admission.Denied(msg)
	}

	availableOperatorConfigurations, errorResponse := loadAvailableOperatorConfigurationResources(ctx, h.Client)
	if errorResponse != nil {
		return *errorResponse
	}
	admissionResponse, done :=
		h.rejectCustomMonitoringResourceInAutomaticallyMonitoredNamespace(
			ctx,
			request.Operation,
			availableOperatorConfigurations,
			monitoringResource,
			logger,
		)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}
	admissionResponse, done = h.validateExport(availableOperatorConfigurations, monitoringResource)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}
	admissionResponse, done = h.validateTelemetryRelatedSettingsIfTelemetryCollectionIsDisabled(availableOperatorConfigurations, monitoringResource)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}
	admissionResponse, done = h.validateLabelSelector(monitoringResource)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}
	admissionResponse, done = h.validateTraceContextPropagators(monitoringResource)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}
	admissionResponse, done = h.validateOttl(monitoringResource)
	if done {
		logger.Info(admissionResponse.Result.Message)
		return admissionResponse
	}

	return admission.Allowed("")
}

func (h *MonitoringValidationWebhookHandler) rejectCustomMonitoringResourceInAutomaticallyMonitoredNamespace(
	ctx context.Context,
	operation admissionv1.Operation,
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) (admission.Response, bool) {
	if len(availableOperatorConfigurations) == 0 {
		// If we cannot check whether autoMonitorNamespaces is enabled on the operator configuration resource, allow the
		// request.
		return admission.Response{}, false
	}
	autoMonitorNamespaces := availableOperatorConfigurations[0].Spec.AutoMonitorNamespaces
	if !autoMonitorNamespaces.IsEnabled() {
		return admission.Response{}, false
	}
	if monitoringResource.Labels[util.AutoMonitoredNamespaceLabel] == "true" {
		// This is a monitoring resource created by the auto_namespace_monitoring_controller, allow it.
		return admission.Response{}, false
	}
	if monitoringResource.Namespace == h.operatorNamespace {
		// Do not reject manually managed monitoring resource in the operator namespace,
		// auto_namespace_monitoring_controller will not install auto-monitoring resources there.
		return admission.Response{}, false
	}
	if slices.Contains(util.RestrictedNamespaces, monitoringResource.Namespace) {
		// Do not reject manually managed monitoring resource in the restricted namespaces,
		// auto_namespace_monitoring_controller will not install auto-monitoring resources there.
		return admission.Response{}, false
	}

	if operation != admissionv1.Create {
		// Allow deleting or updating manually managed resources in auto-monitoring-enabled namespaces, only disallow
		// creating a new manually managed monitoring resource.
		return admission.Response{}, false
	}

	selector, err := labels.Parse(autoMonitorNamespaces.LabelSelector)
	if err != nil {
		// Invalid label selector – be permissive rather than blocking all resources.
		return admission.Response{}, false
	}
	namespace := &corev1.Namespace{}
	if err = h.Client.Get(ctx, client.ObjectKey{Name: monitoringResource.Namespace}, namespace); err != nil {
		logger.Error(
			err,
			fmt.Sprintf("Cannot load namespace %s to validate monitoring resource request.", monitoringResource.Namespace),
		)
		return admission.Response{}, false
	}

	if selector.Matches(labels.Set(namespace.Labels)) {
		return admission.Denied(
			fmt.Sprintf(
				"Namespace \"%s\" is automatically managed by Dash0. Adding a custom Dash0 monitoring resource "+
					"to an automatically managed namespace is not allowed.",
				monitoringResource.Namespace,
			),
		), true
	}

	// Namespace is not subject to automatic monitoring, allow.
	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateExport(
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (admission.Response, bool) {

	// Reject if both the deprecated export and the new exports field are set.
	//nolint:staticcheck
	if monitoringResource.Spec.Export != nil && len(monitoringResource.Spec.Exports) > 0 {
		return admission.Denied(ErrorMessageMonitoringExportAndExportsAreMutuallyExclusive), true
	}

	if len(monitoringResource.Spec.Exports) == 0 {
		if len(availableOperatorConfigurations) == 0 {
			return admission.Denied(
				"The provided Dash0 monitoring resource does not have an export configuration, and no Dash0 operator " +
					"configuration resources are available."), true
		}
		if len(availableOperatorConfigurations) > 1 {
			return admission.Denied(
				"The provided Dash0 monitoring resource does not have an export configuration, and there is more than " +
					"one available Dash0 operator configuration, remove all but one Dash0 operator configuration resource."), true
		}

		operatorConfiguration := availableOperatorConfigurations[0]

		if len(operatorConfiguration.Spec.Exports) == 0 {
			return admission.Denied(
				"The provided Dash0 monitoring resource does not have an export configuration, and the existing Dash0 " +
					"operator configuration does not have an export configuration either."), true
		}
	}

	for _, export := range monitoringResource.Spec.Exports {
		if !validateGrpcExportInsecureFlags(&export) {
			return admission.Denied(ErrorMessageMonitoringGrpcExportInvalidInsecure), true
		}
	}

	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateTelemetryRelatedSettingsIfTelemetryCollectionIsDisabled(
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (admission.Response, bool) {
	if len(availableOperatorConfigurations) == 0 {
		// Since there is no operator configuration available, telemetry collection cannot be disabled via
		// operatorconfiguration.spec.telemetryCollection.enabled=false, hence no further checks for this aspect are
		// necessary.
		return admission.Response{}, false
	}
	operatorConfigurationSpec := availableOperatorConfigurations[0].Spec
	if pointers.ReadBoolPointerWithDefault(operatorConfigurationSpec.TelemetryCollection.Enabled, true) {
		return admission.Response{}, false
	}

	if monitoringResource.Spec.InstrumentWorkloads.Mode != dash0common.InstrumentWorkloadsModeNone {
		return admission.Denied(
			fmt.Sprintf(
				"The Dash0 operator configuration resource has telemetry collection disabled "+
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting "+
					"instrumentWorkloads.mode=%s. This is an invalid combination. Please either set "+
					"telemetryCollection.enabled=true in the operator configuration resource or set "+
					"instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified).",
				monitoringResource.Spec.InstrumentWorkloads.Mode,
			)), true
	}
	if pointers.ReadBoolPointerWithDefault(monitoringResource.Spec.LogCollection.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"logCollection.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"logCollection.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if pointers.ReadBoolPointerWithDefault(monitoringResource.Spec.EventCollection.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"eventCollection.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"eventCollection.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if pointers.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"prometheusScraping.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"prometheusScraping.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if monitoringResource.Spec.Filter != nil {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has filter setting. " +
			"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
			"operator configuration resource or remove the filter setting in the monitoring resource."), true
	}
	if monitoringResource.Spec.Transform != nil {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has a transform setting " +
			"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
			"operator configuration resource or remove the transform setting in the monitoring resource."), true
	}
	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateLabelSelector(monitoringResource *dash0v1beta1.Dash0Monitoring) (admission.Response, bool) {
	labelSelectorRaw := monitoringResource.Spec.InstrumentWorkloads.LabelSelector
	if strings.TrimSpace(labelSelectorRaw) == "" {
		return admission.Denied(
			"The instrumentWorkloads.labelSelector setting in the Dash0 monitoring resource is empty, which is " +
				"invalid. This is a bug in the Dash0 operator. Please report it to Dash0.",
		), true
	}
	_, err := labels.Parse(labelSelectorRaw)
	if err != nil {
		return admission.Denied(fmt.Sprintf(
			"The instrumentWorkloads.labelSelector setting (\"%s\") in the Dash0 monitoring resource is invalid and "+
				"cannot be parsed: %v.", labelSelectorRaw, err)), true
	}
	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateTraceContextPropagators(monitoringResource *dash0v1beta1.Dash0Monitoring) (admission.Response, bool) {
	propagatorsRaw := monitoringResource.Spec.InstrumentWorkloads.TraceContext.Propagators
	if propagatorsRaw == nil || strings.TrimSpace(*propagatorsRaw) == "" {
		return admission.Response{}, false
	}
	propagators := strings.SplitSeq(*propagatorsRaw, ",")
	for propagatorRaw := range propagators {
		propagator := strings.TrimSpace(propagatorRaw)
		if propagator == "" {
			return admission.Denied(
					fmt.Sprintf(
						"The instrumentWorkloads.traceContext.propagators setting (\"%s\") in the Dash0 monitoring "+
							"resource contains an empty value. Please remove the empty value.",
						*propagatorsRaw,
					)),
				true
		}
		if !slices.Contains(validTraceContextPropagators, propagator) {
			return admission.Denied(
					fmt.Sprintf(
						"The instrumentWorkloads.traceContext.propagators setting (\"%s\") in the Dash0 monitoring "+
							"resource contains an unknown propagator value: \"%s\". Valid trace context propagators "+
							"are %s. Please remove the invalid propagator from the list.",
						*propagatorsRaw,
						propagator,
						strings.Join(validTraceContextPropagators, ", "),
					)),
				true
		}
	}
	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateOttl(monitoringResource *dash0v1beta1.Dash0Monitoring) (admission.Response, bool) {
	var errors error

	filter := monitoringResource.Spec.Filter
	if filter != nil {
		errors = multierr.Append(errors, validateFilter(filter))
	}

	normalizedTransformSpec := monitoringResource.Spec.NormalizedTransformSpec
	if normalizedTransformSpec != nil {
		errors = multierr.Append(errors, validateTransform(normalizedTransformSpec))
	}

	if errors != nil {
		return admission.Denied(errors.Error()), true
	}
	return admission.Response{}, false
}

// validateFilter checks the filter conditions of a monitoring resource by rendering them into the configuration of the
// collector's filter processor and running that processor's own validation.
func validateFilter(filter *dash0common.Filter) error {
	return unmarshalAndValidateProcessorConfig(
		filterprocessor.NewFactory().CreateDefaultConfig(),
		renderFilterProcessorConfig(filter),
	)
}

// validateTransform checks the transform statements of a monitoring resource by rendering them into the configuration
// of the collector's transform processor and running that processor's own validation.
func validateTransform(transform *dash0common.NormalizedTransformSpec) error {
	return unmarshalAndValidateProcessorConfig(
		transformprocessor.NewFactory().CreateDefaultConfig(),
		renderTransformProcessorConfig(transform),
	)
}

// unmarshalAndValidateProcessorConfig populates a processor configuration created by its factory from rawConfig and
// then validates it. Unmarshalling rejects unknown keys and invalid enum values, validation parses the OTTL
// expressions, hence both steps need to pass.
func unmarshalAndValidateProcessorConfig(processorConfig component.Config, rawConfig map[string]any) error {
	if err := confmap.NewFromStringMap(rawConfig).Unmarshal(processorConfig); err != nil {
		return err
	}
	return confmap.Validate(processorConfig)
}

// renderFilterProcessorConfig converts the filter settings of a monitoring resource to the configuration of the
// collector's filter processor. The keys need to match
// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/filterprocessor/config.go and
// the filter processor sections of daemonset.config.yaml.template and deployment.config.yaml.template.
func renderFilterProcessorConfig(filter *dash0common.Filter) map[string]any {
	rawConfig := map[string]any{}
	if filter.ErrorMode != "" {
		rawConfig["error_mode"] = string(filter.ErrorMode)
	}
	if filter.Traces != nil {
		traces := map[string]any{}
		addConditions(traces, "span", filter.Traces.SpanFilter)
		addConditions(traces, "spanevent", filter.Traces.SpanEventFilter)
		if len(traces) > 0 {
			rawConfig["traces"] = traces
		}
	}
	if filter.Metrics != nil {
		metrics := map[string]any{}
		addConditions(metrics, "metric", filter.Metrics.MetricFilter)
		addConditions(metrics, "datapoint", filter.Metrics.DataPointFilter)
		if len(metrics) > 0 {
			rawConfig["metrics"] = metrics
		}
	}
	if filter.Logs != nil {
		logs := map[string]any{}
		addConditions(logs, "log_record", filter.Logs.LogRecordFilter)
		if len(logs) > 0 {
			rawConfig["logs"] = logs
		}
	}
	if filter.Profiles != nil {
		profiles := map[string]any{}
		addConditions(profiles, "profile", filter.Profiles.ProfileFilter)
		if len(profiles) > 0 {
			rawConfig["profiles"] = profiles
		}
	}
	return rawConfig
}

func addConditions(signalConfig map[string]any, key string, conditions []string) {
	if len(conditions) == 0 {
		return
	}
	signalConfig[key] = toAnySlice(conditions)
}

// renderTransformProcessorConfig converts the normalized transform settings of a monitoring resource to the
// configuration of the collector's transform processor. The keys need to match
// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/config.go
// and the transform processor sections of daemonset.config.yaml.template and deployment.config.yaml.template.
func renderTransformProcessorConfig(transform *dash0common.NormalizedTransformSpec) map[string]any {
	rawConfig := map[string]any{}
	if transform.ErrorMode != nil {
		rawConfig["error_mode"] = string(*transform.ErrorMode)
	}
	addTransformGroups(rawConfig, "trace_statements", transform.Traces)
	addTransformGroups(rawConfig, "metric_statements", transform.Metrics)
	addTransformGroups(rawConfig, "log_statements", transform.Logs)
	addTransformGroups(rawConfig, "profile_statements", transform.Profiles)
	return rawConfig
}

func addTransformGroups(rawConfig map[string]any, key string, groups []dash0common.NormalizedTransformGroup) {
	if len(groups) == 0 {
		return
	}
	renderedGroups := make([]any, 0, len(groups))
	for _, group := range groups {
		renderedGroup := map[string]any{
			"statements": toAnySlice(group.Statements),
		}
		if group.Context != nil {
			renderedGroup["context"] = *group.Context
		}
		if group.ErrorMode != nil {
			renderedGroup["error_mode"] = string(*group.ErrorMode)
		}
		if len(group.Conditions) > 0 {
			renderedGroup["conditions"] = toAnySlice(group.Conditions)
		}
		renderedGroups = append(renderedGroups, renderedGroup)
	}
	rawConfig[key] = renderedGroups
}

func toAnySlice(values []string) []any {
	converted := make([]any, 0, len(values))
	for _, value := range values {
		converted = append(converted, value)
	}
	return converted
}
