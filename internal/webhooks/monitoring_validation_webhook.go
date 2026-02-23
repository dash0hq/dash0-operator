// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"go.opentelemetry.io/collector/component"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/internal_/filter/filterottl"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/common"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/logs"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/metrics"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/traces"
)

const ErrorMessageMonitoringGrpcExportInvalidInsecure = "The provided Dash0 monitoring resource has both insecure and insecureSkipVerify " +
	"explicitly enabled for the GRPC export. This is an invalid combination. " +
	"Please set at most one of these two flags to true."

const ErrorMessageMonitoringExportAndExportsAreMutuallyExclusive = "The provided Dash0 monitoring resource has both the " +
	"deprecated `export` and the `exports` field set. These fields are mutually exclusive. Please use only the " +
	"`exports` field and remove the `export` field."

type MonitoringValidationWebhookHandler struct {
	Client client.Client
}

func NewMonitoringValidationWebhookHandler(
	k8sClient client.Client,
) *MonitoringValidationWebhookHandler {
	return &MonitoringValidationWebhookHandler{
		Client: k8sClient,
	}
}

var (
	restrictedNamespaces = []string{
		"kube-system",
		"kube-node-lease",
	}
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
	logger := log.FromContext(ctx)

	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, monitoringResource); err != nil {
		logger.Info("rejecting invalid monitoring resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	instrumentWorkloadsMode := monitoringResource.Spec.InstrumentWorkloads.Mode
	if slices.Contains(restrictedNamespaces, request.Namespace) && instrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeNone {
		msg := fmt.Sprintf(
			"Rejecting the deployment of Dash0 monitoring resource \"%s\" to the Kubernetes system namespace "+
				"\"%s\" with instrumentWorkloads.mode=%s, use instrumentWorkloads.mode=none instead.",
			request.Name,
			request.Namespace,
			instrumentWorkloadsMode,
		)
		logger.Info(msg)
		return admission.Denied(msg)
	}

	availableOperatorConfigurations, errorResponse := loadAvailableOperatorConfigurationResources(ctx, h.Client)
	if errorResponse != nil {
		return *errorResponse
	}
	admissionResponse, done := h.validateExport(availableOperatorConfigurations, monitoringResource)
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
	if util.ReadBoolPointerWithDefault(operatorConfigurationSpec.TelemetryCollection.Enabled, true) {
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
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.LogCollection.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"logCollection.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"logCollection.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.EventCollection.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"eventCollection.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"eventCollection.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
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
	// Note: Due to dash0common.Filter and dash0common.Transform using different types than the Config modules in
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/filterprocessor and
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/transformprocessor,
	// we need to vendor in quite a bit of internal code from the collector-contrib repo, see
	// internal/webhooks/vendored/opentelemetry-collector-contrib.

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

// validateFilter is a modified copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.126.0/processor/filterprocessor/config.go,
// func (cfg *Config) Validate() error {
func validateFilter(filter *dash0common.Filter) error {
	var errors error

	if filter.Traces != nil {
		if filter.Traces.SpanFilter != nil {
			_, err := filterottl.NewBoolExprForSpan(filter.Traces.SpanFilter, filterottl.StandardSpanFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
			errors = multierr.Append(errors, err)
		}
		if filter.Traces.SpanEventFilter != nil {
			_, err := filterottl.NewBoolExprForSpanEvent(filter.Traces.SpanEventFilter, filterottl.StandardSpanEventFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
			errors = multierr.Append(errors, err)
		}
	}

	if filter.Metrics != nil {
		if filter.Metrics.MetricFilter != nil {
			_, err := filterottl.NewBoolExprForMetric(filter.Metrics.MetricFilter, filterottl.StandardMetricFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
			errors = multierr.Append(errors, err)
		}
		if filter.Metrics.DataPointFilter != nil {
			_, err := filterottl.NewBoolExprForDataPoint(filter.Metrics.DataPointFilter, filterottl.StandardDataPointFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
			errors = multierr.Append(errors, err)
		}
	}

	if filter.Logs != nil {
		if filter.Logs.LogRecordFilter != nil {
			_, err := filterottl.NewBoolExprForLog(filter.Logs.LogRecordFilter, filterottl.StandardLogFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
			errors = multierr.Append(errors, err)
		}
	}

	return errors
}

// validateTransform is a modified copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.126.0/processor/transformprocessor/config.go,
// func (cfg *Config) Validate() error {
func validateTransform(transform *dash0common.NormalizedTransformSpec) error {
	var errors error

	if len(transform.Traces) > 0 {
		pc, err := common.NewTraceParserCollection(component.TelemetrySettings{Logger: zap.NewNop()}, common.WithSpanParser(traces.SpanFunctions()), common.WithSpanEventParser(traces.SpanEventFunctions()))
		if err != nil {
			return err
		}
		for _, cs := range transform.Traces {
			_, err = pc.ParseContextStatements(toContextStatements(cs))
			if err != nil {
				errors = multierr.Append(errors, err)
			}
		}
	}

	if len(transform.Metrics) > 0 {
		pc, err := common.NewMetricParserCollection(component.TelemetrySettings{Logger: zap.NewNop()}, common.WithMetricParser(metrics.MetricFunctions()), common.WithDataPointParser(metrics.DataPointFunctions()))
		if err != nil {
			return err
		}
		for _, cs := range transform.Metrics {
			_, err := pc.ParseContextStatements(toContextStatements(cs))
			if err != nil {
				errors = multierr.Append(errors, err)
			}
		}
	}

	if len(transform.Logs) > 0 {
		pc, err := common.NewLogParserCollection(component.TelemetrySettings{Logger: zap.NewNop()}, common.WithLogParser(logs.LogFunctions()))
		if err != nil {
			return err
		}
		for _, cs := range transform.Logs {
			_, err = pc.ParseContextStatements(toContextStatements(cs))
			if err != nil {
				errors = multierr.Append(errors, err)
			}
		}
	}

	return errors
}

func toContextStatements(transformGroup dash0common.NormalizedTransformGroup) common.ContextStatements {
	var ctx common.ContextID
	if transformGroup.Context != nil {
		ctx = common.ContextID(*transformGroup.Context)
	}
	var errorMode ottl.ErrorMode
	if transformGroup.ErrorMode != nil {
		errorMode = ottl.ErrorMode(*transformGroup.ErrorMode)
	}
	return common.ContextStatements{
		Context:    ctx,
		Conditions: transformGroup.Conditions,
		Statements: transformGroup.Statements,
		ErrorMode:  errorMode,
	}
}
