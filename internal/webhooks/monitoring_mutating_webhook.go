// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type MonitoringMutatingWebhookHandler struct {
	Client            client.Client
	OperatorNamespace string
}

func (h *MonitoringMutatingWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/monitoring/mutate", handler)

	return nil
}

func (h *MonitoringMutatingWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	// Note: The mutating webhook is called before the validating webhook, so we can normalize the resource here and
	// verify that it is valid (after having been normalized) in the validating webhook.
	// Note that default values from // +kubebuilder:default comments from
	// api/operator/v1alpha1/dash0monitoring_types.go have already been applied by the time this webhook
	// is called.
	// See https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#admission-control-phases.
	logger := log.FromContext(ctx)

	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, monitoringResource); err != nil {
		logger.Info("rejecting invalid monitoring resource", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	availableOperatorConfigurations, errorResponse := loadAvailableOperatorConfigurationResources(ctx, h.Client)
	if errorResponse != nil {
		return *errorResponse
	}
	var operatorConfigurationSpec *dash0v1alpha1.Dash0OperatorConfigurationSpec
	if len(availableOperatorConfigurations) > 0 {
		operatorConfigurationSpec = &availableOperatorConfigurations[0].Spec
	}

	patchRequired, errorResponse := h.normalizeMonitoringResourceSpec(request, operatorConfigurationSpec, &monitoringResource.Spec, &logger)

	if errorResponse != nil {
		return *errorResponse
	}

	if !patchRequired {
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(monitoringResource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied monitoring resource to JSON: %w", err)
		return admission.Errored(http.StatusInternalServerError, wrappedErr)
	}

	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *MonitoringMutatingWebhookHandler) normalizeMonitoringResourceSpec(
	request admission.Request,
	operatorConfigurationSpec *dash0v1alpha1.Dash0OperatorConfigurationSpec,
	monitoringSpec *dash0v1beta1.Dash0MonitoringSpec,
	logger *logr.Logger,
) (bool, *admission.Response) {
	patchRequired := h.setTelemetryCollectionRelatedDefaults(request, operatorConfigurationSpec, monitoringSpec)
	patchRequiredForLogCollection := h.overrideLogCollectionDefault(request, monitoringSpec, logger)
	patchRequired = patchRequired || patchRequiredForLogCollection

	// Normalize spec.transform to the transform processors "advanced" config format.
	transform := monitoringSpec.Transform
	if transform != nil {
		var responseStatus int32
		var err error
		monitoringSpec.NormalizedTransformSpec, responseStatus, err = normalizeTransform(transform, logger)
		if err != nil {
			errorResponse := admission.Errored(responseStatus, err)
			return false, &errorResponse
		}
		patchRequired = true
	}

	return patchRequired, nil
}

func (h *MonitoringMutatingWebhookHandler) setTelemetryCollectionRelatedDefaults(
	request admission.Request,
	operatorConfigurationSpec *dash0v1alpha1.Dash0OperatorConfigurationSpec,
	monitoringSpec *dash0v1beta1.Dash0MonitoringSpec,
) bool {
	patchRequired := false
	telemetryCollectionEnabled := true
	if operatorConfigurationSpec != nil {
		telemetryCollectionEnabled = util.ReadBoolPointerWithDefault(operatorConfigurationSpec.TelemetryCollection.Enabled, true)
	}

	if monitoringSpec.InstrumentWorkloads.Mode == "" {
		if telemetryCollectionEnabled {
			monitoringSpec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeAll
		} else {
			monitoringSpec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeNone
		}
		patchRequired = true
	}
	if monitoringSpec.LogCollection.Enabled == nil {
		if request.Namespace == h.OperatorNamespace {
			monitoringSpec.LogCollection.Enabled = ptr.To(false)
		} else {
			monitoringSpec.LogCollection.Enabled = ptr.To(telemetryCollectionEnabled)
		}
		patchRequired = true
	}
	if monitoringSpec.PrometheusScraping.Enabled == nil {
		monitoringSpec.PrometheusScraping.Enabled = ptr.To(telemetryCollectionEnabled)
		patchRequired = true
	}
	return patchRequired
}

func (h *MonitoringMutatingWebhookHandler) overrideLogCollectionDefault(
	request admission.Request,
	monitoringSpec *dash0v1beta1.Dash0MonitoringSpec,
	logger *logr.Logger,
) bool {
	if request.Namespace == h.OperatorNamespace &&
		util.ReadBoolPointerWithDefault(monitoringSpec.LogCollection.Enabled, true) {
		logger.Info(
			fmt.Sprintf(
				"Automatically disabling log collection in the operator namespace %s. Logs from the operator can be "+
					"collected via self monitoring, see "+
					"https://github.com/dash0hq/dash0-operator/tree/main/helm-chart/dash0-operator#operatorconfigurationresource.spec.selfMonitoring.enabled. "+
					"Collecting them via the filelog receiver is not supported. You can get rid of this log message "+
					"by explicitly disabling log collection for this namespace, see "+
					"https://github.com/dash0hq/dash0-operator/tree/main/helm-chart/dash0-operator#monitoringresource.spec.logCollection.enabled.",
				h.OperatorNamespace))
		monitoringSpec.LogCollection.Enabled = ptr.To(false)
		return true
	}
	return false
}

func normalizeTransform(transform *dash0common.Transform, logger *logr.Logger) (*dash0common.NormalizedTransformSpec, int32, error) {
	traceTransformGroups, responseStatus, err :=
		normalizeTransformGroupsForOneSignal(transform.Traces, "trace_statements", logger)
	if err != nil {
		logger.Error(err, "error when normalizing transform.trace_statements")
		return nil, responseStatus, err
	}
	metricTransformGroups, responseStatus, err :=
		normalizeTransformGroupsForOneSignal(transform.Metrics, "metric_statements", logger)
	if err != nil {
		logger.Error(err, "error when normalizing transform.metric_statements")
		return nil, responseStatus, err
	}
	logTransformGroups, responseStatus, err :=
		normalizeTransformGroupsForOneSignal(transform.Logs, "log_statements", logger)
	if err != nil {
		logger.Error(err, "error when normalizing transform.log_statements")
		return nil, responseStatus, err
	}

	return &dash0common.NormalizedTransformSpec{
		ErrorMode: transform.ErrorMode,
		Traces:    traceTransformGroups,
		Metrics:   metricTransformGroups,
		Logs:      logTransformGroups,
	}, 0, nil
}

func normalizeTransformGroupsForOneSignal(
	signalTransformSpec []json.RawMessage,
	signalTypeKey string,
	logger *logr.Logger,
) ([]dash0common.NormalizedTransformGroup, int32, error) {
	var allGroups []dash0common.NormalizedTransformGroup
	for ctxIdx, transformGroup := range signalTransformSpec {
		jsonPayload, err := transformGroup.MarshalJSON()
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("extracting the JSON payload for spec.transform.%s[%d] failed: %w", signalTypeKey, ctxIdx, err)
		}

		var groupUnmarshalled interface{}
		if err = json.Unmarshal(jsonPayload, &groupUnmarshalled); err != nil {
			return nil,
				http.StatusBadRequest,
				fmt.Errorf("parsing the JSON payload for spec.transform.%s[%d] failed: %w", signalTypeKey, ctxIdx, err)
		}
		flatStatement, isString := groupUnmarshalled.(string)
		if isString {
			allGroups = append(allGroups, dash0common.NormalizedTransformGroup{
				Statements: []string{flatStatement},
			})
			continue
		}

		groupAsMap, isMap := groupUnmarshalled.(map[string]interface{})
		if isMap {
			normalizedGroup := dash0common.NormalizedTransformGroup{}
			if contextSpec, hasContext := groupAsMap["context"]; hasContext && contextSpec != nil {
				ctxSpecString := contextSpec.(string)
				normalizedGroup.Context = &ctxSpecString
			}
			if errorModeRaw, hasErrorMode := groupAsMap["error_mode"]; hasErrorMode {
				if errorMode, ok := errorModeRaw.(string); ok {
					em := dash0common.FilterTransformErrorMode(errorMode)
					normalizedGroup.ErrorMode = &em
				} else {
					logger.Error(
						err,
						fmt.Sprintf(
							"ignoring invalid error mode %v for spec.transform.%s[%d]: ",
							errorModeRaw,
							signalTypeKey,
							ctxIdx,
						))
				}
			}
			if conditionsRaw, hasConditions := groupAsMap["conditions"]; hasConditions {
				if conditionsI, listOk := conditionsRaw.([]interface{}); listOk {
					conditions := make([]string, len(conditionsI))
					for conditionIdx, condition := range conditionsI {
						if conditionS, elemOk := condition.(string); elemOk {
							conditions[conditionIdx] = conditionS
						} else {
							return nil, http.StatusBadRequest, fmt.Errorf(
								"invalid condition %v for spec.transform.%s[%d][%d], not a string: %w",
								condition,
								signalTypeKey,
								ctxIdx,
								conditionIdx,
								err,
							)
						}
					}
					normalizedGroup.Conditions = conditions
				} else {
					return nil, http.StatusBadRequest, fmt.Errorf(
						"invalid conditions %v for spec.transform.%s[%d], not a list: %w",
						conditionsRaw,
						signalTypeKey,
						ctxIdx,
						err,
					)
				}
			}
			if statementsRaw, hasStatements := groupAsMap["statements"]; hasStatements {
				if statementsI, listOk := statementsRaw.([]interface{}); listOk {
					statements := make([]string, len(statementsI))
					for statementIdx, statement := range statementsI {
						if statementS, elemOk := statement.(string); elemOk {
							statements[statementIdx] = statementS
						} else {
							return nil, http.StatusBadRequest, fmt.Errorf(
								"invalid statements %v for spec.transform.%s[%d][%d], not a string: %w",
								statement,
								signalTypeKey,
								ctxIdx,
								statementIdx,
								err,
							)
						}
					}
					normalizedGroup.Statements = statements
				} else {
					return nil, http.StatusBadRequest, fmt.Errorf(
						"invalid statements %v for spec.transform.%s[%d], not a list: %w",
						statementsRaw,
						signalTypeKey,
						ctxIdx,
						err,
					)
				}
			}
			allGroups = append(allGroups, normalizedGroup)
			continue
		}

		return nil, http.StatusBadRequest,
			fmt.Errorf("unsupported spec.transform.%s[%d] format: %s", signalTypeKey, ctxIdx, jsonPayload)
	}
	return allGroups, 0, nil
}

func loadAvailableOperatorConfigurationResources(
	ctx context.Context,
	k8sClient client.Client,
) ([]dash0v1alpha1.Dash0OperatorConfiguration, *admission.Response) {
	operatorConfigurationList := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := k8sClient.List(ctx, operatorConfigurationList, &client.ListOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			errorResponse := admission.Errored(http.StatusInternalServerError, err)
			return nil, &errorResponse
		}
	}
	var availableOperatorConfigurations []dash0v1alpha1.Dash0OperatorConfiguration
	for _, operatorConfiguration := range operatorConfigurationList.Items {
		if operatorConfiguration.IsAvailable() {
			availableOperatorConfigurations = append(availableOperatorConfigurations, operatorConfiguration)
		}
	}
	return availableOperatorConfigurations, nil
}
