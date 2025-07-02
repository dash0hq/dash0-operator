// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"go.opentelemetry.io/collector/component"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	log "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/internal_/filter/filterottl"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/common"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/logs"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/metrics"
	"github.com/dash0hq/dash0-operator/internal/webhooks/vendored/opentelemetry-collector-contrib/processor/transformprocessor/internal_/traces"
)

type MonitoringValidationWebhookHandler struct {
	Client client.Client
}

var (
	restrictedNamespaces = []string{
		"kube-system",
		"kube-node-lease",
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
	mgr.GetWebhookServer().Register("/v1alpha1/validate/monitoring", handler)

	return nil
}

func (h *MonitoringValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can assume the resource has already
	// been normalized by the mutating webhook.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	logger := log.FromContext(ctx)

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
		logger.Info("Warning: The setting Dash0Monitoring.spec.prometheusScrapingEnabled is deprecated: Please use " +
			"Dash0Monitoring.spec.prometheusScraping.enabled instead.")
	}

	if slices.Contains(restrictedNamespaces, request.Namespace) && monitoringResource.Spec.InstrumentWorkloads != dash0v1alpha1.None {
		return admission.Denied(
			fmt.Sprintf(
				"Rejecting the deployment of Dash0 monitoring resource \"%s\" to the Kubernetes system namespace "+
					"\"%s\" with instrumentWorkloads=%s, use instrumentWorkloads=none instead.",
				request.Name,
				request.Namespace,
				monitoringResource.Spec.InstrumentWorkloads,
			))
	}

	availableOperatorConfigurations, errorResponse := loadAvailableOperatorConfigurationResources(ctx, h.Client)
	if errorResponse != nil {
		return *errorResponse
	}

	admissionResponse, done := h.validateExport(availableOperatorConfigurations, monitoringResource)
	if done {
		return admissionResponse
	}
	admissionResponse, done = h.validateTelemetryRelatedSettingsIfTelemetryCollectionIsDisabled(availableOperatorConfigurations, monitoringResource)
	if done {
		return admissionResponse
	}

	admissionResponse, done = h.validateOttl(monitoringResource)
	if done {
		return admissionResponse
	}

	return admission.Allowed("")
}

func (h *MonitoringValidationWebhookHandler) validateExport(
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
) (admission.Response, bool) {
	if monitoringResource.Spec.Export == nil {
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

		opperatorConfiguration := availableOperatorConfigurations[0]

		if opperatorConfiguration.Spec.Export == nil {
			return admission.Denied(
				"The provided Dash0 monitoring resource does not have an export configuration, and the existing Dash0 " +
					"operator configuration does not have an export configuration either."), true
		}
	}
	return admission.Response{}, false
}

func (h *MonitoringValidationWebhookHandler) validateTelemetryRelatedSettingsIfTelemetryCollectionIsDisabled(
	availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
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

	if monitoringResource.Spec.InstrumentWorkloads != dash0v1alpha1.None {
		return admission.Denied(
			fmt.Sprintf(
				"The Dash0 operator configuration resource has telemetry collection disabled "+
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting "+
					"instrumentWorkloads=%s. This is an invalid combination. Please either set "+
					"telemetryCollection.enabled=true in the operator configuration resource or set "+
					"instrumentWorkloads=none in the monitoring resource (or leave it unspecified).",
				monitoringResource.Spec.InstrumentWorkloads,
			)), true
	}
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.LogCollection.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"logCollection.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"logCollection.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"prometheusScraping.enabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"prometheusScraping.enabled=false in the monitoring resource (or leave it unspecified)."), true
	}
	//nolint:staticcheck
	if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScrapingEnabled, true) {
		return admission.Denied("The Dash0 operator configuration resource has telemetry collection disabled " +
			"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
			"prometheusScrapingEnabled=true. This is an invalid combination. Please either set " +
			"telemetryCollection.enabled=true in the operator configuration resource or set " +
			"prometheusScrapingEnabled=false in the monitoring resource (or leave it unspecified)."), true
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

func (h *MonitoringValidationWebhookHandler) validateOttl(monitoringResource *dash0v1alpha1.Dash0Monitoring) (admission.Response, bool) {
	// Note: Due to dash0v1alpha1.Filter and dash0v1alpha1.Transform using different types than the Config modules in
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/filterprocessor and
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/transformprocessor,
	// we need to vendor in quite a bit of internal code from the collector-contrib repo, see
	// internal/webhooks/vendored/opentelemetry-collector-contrib. Would be worth trying to refactor
	// dash0v1alpha1.Filter and dash0v1alpha1.Transform to directly use the types from the collector-contrib repo, then
	// we might be able to get away with only using collector-contrib's public API only, in particular,
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/filterprocessor/config.go,
	// func (cfg *Config) Validate() error, and
	// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.126.0/processor/transformprocessor/config.go,
	// func (cfg *Config) Validate() error.
	if monitoringResource.Spec.Filter != nil {
		if err := validateFilter(monitoringResource.Spec.Filter); err != nil {
			return admission.Denied(err.Error()), true
		}
	}
	if monitoringResource.Spec.NormalizedTransformSpec != nil {
		if err := validateTransform(monitoringResource.Spec.NormalizedTransformSpec); err != nil {
			return admission.Denied(err.Error()), true
		}
	}
	return admission.Response{}, false
}

// validateFilter is a modified copy of
// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.126.0/processor/filterprocessor/config.go,
// func (cfg *Config) Validate() error {
func validateFilter(filter *dash0v1alpha1.Filter) error {
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
func validateTransform(transform *dash0v1alpha1.NormalizedTransformSpec) error {
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

func toContextStatements(transformGroup dash0v1alpha1.NormalizedTransformGroup) common.ContextStatements {
	var context common.ContextID
	if transformGroup.Context != nil {
		context = common.ContextID(*transformGroup.Context)
	}
	var errorMode ottl.ErrorMode
	if transformGroup.ErrorMode != nil {
		errorMode = ottl.ErrorMode(*transformGroup.ErrorMode)
	}
	return common.ContextStatements{
		Context:    context,
		Conditions: transformGroup.Conditions,
		Statements: transformGroup.Statements,
		ErrorMode:  errorMode,
	}
}
