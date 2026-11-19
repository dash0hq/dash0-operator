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

// ErrorMessageMonitoringInstrumentationDisabled is the message for rejecting a monitoring resource with
// instrumentWorkloads.mode other than none while workload instrumentation is disabled.
const ErrorMessageMonitoringInstrumentationDisabled = "Rejecting the Dash0 monitoring resource \"%s\" in the " +
	"namespace \"%s\" with instrumentWorkloads.mode=%s, since workload instrumentation is disabled via the Helm " +
	"value %s. Use instrumentWorkloads.mode=none instead."

// ErrorMessageMonitoringInstrumentationDisabledUpdate is the message for rejecting an update of a monitoring resource
// that changes instrumentWorkloads.mode to a value other than none while workload instrumentation is disabled.
const ErrorMessageMonitoringInstrumentationDisabledUpdate = "Rejecting the update of the Dash0 monitoring resource " +
	"\"%s\" in the namespace \"%s\", which changes instrumentWorkloads.mode to %s, since workload instrumentation is " +
	"disabled via the Helm value %s. While workload instrumentation is disabled, instrumentWorkloads.mode can only " +
	"be changed to none."

const ErrorMessageMonitoringExportAndExportsAreMutuallyExclusive = "The provided Dash0 monitoring resource has both the " +
	"deprecated `export` and the `exports` field set. These fields are mutually exclusive. Please use only the " +
	"`exports` field and remove the `export` field."

type MonitoringValidationWebhookHandler struct {
	Client                            client.Client
	operatorNamespace                 string
	telemetryCollectionEnabledViaHelm bool
	instrumentationEnabled            bool
}

// NewMonitoringValidationWebhookHandler creates the validation webhook handler for monitoring resources. If
// telemetryCollectionEnabledViaHelm is false, monitoring resources that enable instrumentation or telemetry collection
// are rejected. If instrumentationEnabled is false, monitoring resources with instrumentWorkloads.mode other than none
// are rejected.
func NewMonitoringValidationWebhookHandler(
	k8sClient client.Client,
	operatorNamespace string,
	telemetryCollectionEnabledViaHelm bool,
	instrumentationEnabled bool,
) *MonitoringValidationWebhookHandler {
	return &MonitoringValidationWebhookHandler{
		Client:                            k8sClient,
		operatorNamespace:                 operatorNamespace,
		telemetryCollectionEnabledViaHelm: telemetryCollectionEnabledViaHelm,
		instrumentationEnabled:            instrumentationEnabled,
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
	if isReachabilityCheck(request) {
		return admission.Allowed("")
	}
	logger := logd.FromContext(ctx)

	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, monitoringResource); err != nil {
		logger.Warn("rejecting invalid monitoring resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	instrumentWorkloadsMode := monitoringResource.Spec.InstrumentWorkloads.Mode
	if request.Operation == admissionv1.Update &&
		!slices.Contains(monitoringResource.Finalizers, dash0common.MonitoringFinalizerId) {
		// Always allow requests that remove the finalizer. Otherwise, we might accidentally block removing a manually
		// managed monitoring resource from a namespace that is enabled for auto-namespace monitoring. The exception is
		// a request that changes instrumentWorkloads.mode to a value other than none in a namespace that only allows
		// mode none, or while workload instrumentation is disabled.
		changesRestrictedMode, errorResponse := h.changesInstrumentWorkloadsModeWhereOnlyNoneIsAllowed(
			request,
			monitoringResource,
		)
		if errorResponse != nil {
			logger.Warn("rejecting monitoring resource update", "error", errorResponse.Result.Message)
			return *errorResponse
		}
		if !changesRestrictedMode {
			return admission.Allowed("")
		}
	}

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
	if !h.instrumentationEnabled && instrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeNone {
		rejectMode := true
		messageTemplate := ErrorMessageMonitoringInstrumentationDisabled
		if request.Operation == admissionv1.Update {
			// A resource that has a mode other than none from before workload instrumentation has been disabled can still
			// be updated, as long as the update keeps the mode as it is.
			modeChanged, errorResponse := instrumentWorkloadsModeChanged(request, monitoringResource)
			if errorResponse != nil {
				return *errorResponse
			}
			rejectMode = modeChanged
			messageTemplate = ErrorMessageMonitoringInstrumentationDisabledUpdate
		}
		if rejectMode {
			msg := fmt.Sprintf(
				messageTemplate,
				request.Name,
				request.Namespace,
				instrumentWorkloadsMode,
				instrumentationDisabledHelmValue(h.telemetryCollectionEnabledViaHelm),
			)
			logger.Warn(msg)
			return admission.Denied(msg)
		}
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
	admissionResponse, done = h.validateTelemetryRelatedSettingsIfTelemetryCollectionIsDisabled(
		request,
		availableOperatorConfigurations,
		monitoringResource,
	)
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

	return admission.Allowed("").
		WithWarnings(deprecatedLogRecordsFilterWarnings(monitoringResource.Spec.Filter, "spec.filter")...)
}

// instrumentationDisabledHelmValue returns the Helm value that has disabled workload instrumentation, for use in
// messages. Disabling telemetry collection via Helm also disables workload instrumentation.
func instrumentationDisabledHelmValue(telemetryCollectionEnabledViaHelm bool) string {
	if !telemetryCollectionEnabledViaHelm {
		return "operator.telemetryCollectionEnabled=false"
	}
	return "operator.instrumentation.enabled=false"
}

// changesInstrumentWorkloadsModeWhereOnlyNoneIsAllowed checks whether an update request changes the effective
// instrumentWorkloads.mode to a value other than none where only mode none is allowed, that is, in a Kubernetes system
// namespace, in the operator namespace, or in any namespace while workload instrumentation is disabled.
func (h *MonitoringValidationWebhookHandler) changesInstrumentWorkloadsModeWhereOnlyNoneIsAllowed(
	request admission.Request,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (bool, *admission.Response) {
	if h.instrumentationEnabled &&
		!slices.Contains(util.RestrictedNamespaces, request.Namespace) &&
		request.Namespace != h.operatorNamespace {
		return false, nil
	}
	if monitoringResource.Spec.InstrumentWorkloads.Mode == dash0common.InstrumentWorkloadsModeNone {
		return false, nil
	}
	return instrumentWorkloadsModeChanged(request, monitoringResource)
}

// instrumentWorkloadsModeChanged checks whether an update request changes the effective instrumentWorkloads.mode.
func instrumentWorkloadsModeChanged(
	request admission.Request,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (bool, *admission.Response) {
	oldMonitoringResource, errorResponse := decodeOldMonitoringResource(request)
	if errorResponse != nil {
		return false, errorResponse
	}
	return oldMonitoringResource.ReadInstrumentWorkloadsMode() != monitoringResource.ReadInstrumentWorkloadsMode(), nil
}

// decodeOldMonitoringResource decodes the previous version of the monitoring resource from an update request.
func decodeOldMonitoringResource(request admission.Request) (*dash0v1beta1.Dash0Monitoring, *admission.Response) {
	oldMonitoringResource := &dash0v1beta1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.OldObject.Raw, nil, oldMonitoringResource); err != nil {
		response := admission.Errored(
			http.StatusBadRequest,
			fmt.Errorf("cannot decode the previous version of the monitoring resource: %w", err),
		)
		return nil, &response
	}
	return oldMonitoringResource, nil
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
	request admission.Request,
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (admission.Response, bool) {
	var reason, remedy string
	var oldMonitoringResource *dash0v1beta1.Dash0Monitoring
	if !h.telemetryCollectionEnabledViaHelm {
		// The operator configuration resource might not exist yet, or it might be managed manually without having set
		// telemetryCollection.enabled=false. Telemetry collection cannot be enabled there anyway if it has been
		// disabled via Helm.
		reason = "Telemetry collection is disabled via the Helm value operator.telemetryCollectionEnabled=false"
		remedy = "set operator.telemetryCollectionEnabled=true via Helm"
		if request.Operation == admissionv1.Update {
			// Only reject settings that the update turns on, so that resources which have been created before telemetry
			// collection has been disabled via Helm can still be updated.
			var errorResponse *admission.Response
			oldMonitoringResource, errorResponse = decodeOldMonitoringResource(request)
			if errorResponse != nil {
				return *errorResponse, true
			}
		}
	} else if len(availableOperatorConfigurations) > 0 &&
		!pointers.ReadBoolPointerWithDefault(availableOperatorConfigurations[0].Spec.TelemetryCollection.Enabled, true) {
		reason = "The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false)"
		remedy = "set telemetryCollection.enabled=true in the operator configuration resource"
	} else {
		return admission.Response{}, false
	}

	conflictingSetting := func(setting string, resolution string) (admission.Response, bool) {
		return admission.Denied(fmt.Sprintf(
			"%s, and yet the monitoring resource has the setting %s. This is an invalid combination. Please either "+
				"%s or %s.",
			reason,
			setting,
			remedy,
			resolution,
		)), true
	}

	telemetryRelatedSettings := []struct {
		setting    string
		resolution string
		isSet      func(*dash0v1beta1.Dash0Monitoring) bool
	}{
		{
			setting:    fmt.Sprintf("instrumentWorkloads.mode=%s", monitoringResource.Spec.InstrumentWorkloads.Mode),
			resolution: "set instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified)",
			isSet: func(m *dash0v1beta1.Dash0Monitoring) bool {
				return m.Spec.InstrumentWorkloads.Mode != dash0common.InstrumentWorkloadsModeNone
			},
		},
		{
			setting:    "logCollection.enabled=true",
			resolution: "set logCollection.enabled=false in the monitoring resource (or leave it unspecified)",
			isSet: func(m *dash0v1beta1.Dash0Monitoring) bool {
				return pointers.ReadBoolPointerWithDefault(m.Spec.LogCollection.Enabled, true)
			},
		},
		{
			setting:    "eventCollection.enabled=true",
			resolution: "set eventCollection.enabled=false in the monitoring resource (or leave it unspecified)",
			isSet: func(m *dash0v1beta1.Dash0Monitoring) bool {
				return pointers.ReadBoolPointerWithDefault(m.Spec.EventCollection.Enabled, true)
			},
		},
		{
			setting: "kubernetesObjectCollection.enabled=true",
			resolution: "set kubernetesObjectCollection.enabled=false in the monitoring resource (or leave it " +
				"unspecified)",
			isSet: func(m *dash0v1beta1.Dash0Monitoring) bool {
				// Kubernetes object collection is opt-in, so unlike the other settings an unspecified value never
				// conflicts; the mutating webhook does not derive it from telemetryCollection.enabled either.
				return pointers.ReadBoolPointerWithDefault(m.Spec.KubernetesObjectCollection.Enabled, false)
			},
		},
		{
			setting:    "prometheusScraping.enabled=true",
			resolution: "set prometheusScraping.enabled=false in the monitoring resource (or leave it unspecified)",
			isSet: func(m *dash0v1beta1.Dash0Monitoring) bool {
				return pointers.ReadBoolPointerWithDefault(m.Spec.PrometheusScraping.Enabled, true)
			},
		},
		{
			setting:    "filter",
			resolution: "remove the filter setting in the monitoring resource",
			isSet:      func(m *dash0v1beta1.Dash0Monitoring) bool { return m.Spec.Filter != nil },
		},
		{
			setting:    "transform",
			resolution: "remove the transform setting in the monitoring resource",
			isSet:      func(m *dash0v1beta1.Dash0Monitoring) bool { return m.Spec.Transform != nil },
		},
	}
	for _, telemetryRelatedSetting := range telemetryRelatedSettings {
		if telemetryRelatedSetting.isSet(monitoringResource) &&
			(oldMonitoringResource == nil || !telemetryRelatedSetting.isSet(oldMonitoringResource)) {
			return conflictingSetting(telemetryRelatedSetting.setting, telemetryRelatedSetting.resolution)
		}
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

// deprecatedLogRecordsFilterWarnings returns an admission warning if the given filter uses the deprecated key
// logs.log_records instead of logs.log_record. The path is the location of the filter in the resource, e.g.
// "spec.filter".
func deprecatedLogRecordsFilterWarnings(filter *dash0common.Filter, path string) []string {
	//nolint:staticcheck
	if filter == nil || filter.Logs == nil || len(filter.Logs.DeprecatedLogRecordFilter) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%[1]s.logs.log_records is deprecated, use %[1]s.logs.log_record instead.", path)}
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
		addConditions(logs, "log_record", filter.Logs.EffectiveLogRecordFilter())
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
