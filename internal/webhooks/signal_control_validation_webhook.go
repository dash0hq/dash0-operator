// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sync/atomic"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

const (
	durationBoundMin = 10 * time.Second
	durationBoundMax = 1 * time.Hour
)

type SignalControlValidationWebhookHandler struct {
	Client              client.Client
	extraConfig         atomic.Pointer[util.ExtraConfig]
	extraConfigDefaults util.ExtraConfig
}

// NewSignalControlValidationWebhookHandler creates the validation webhook handler for Signal Control resources. The
// extra configuration and its defaults are used to warn about settings in spec.components that override a value
// provided via the Helm chart, and to check the ephemeral-storage limit of the Signal Control collector against the
// disk-backed trace reservoir.
func NewSignalControlValidationWebhookHandler(
	k8sClient client.Client,
	extraConfig util.ExtraConfig,
	extraConfigDefaults util.ExtraConfig,
) *SignalControlValidationWebhookHandler {
	h := &SignalControlValidationWebhookHandler{
		Client:              k8sClient,
		extraConfigDefaults: extraConfigDefaults,
	}
	h.extraConfig.Store(&extraConfig)
	return h
}

// UpdateExtraConfig stores the updated extra config map, which spec.components is checked against.
func (h *SignalControlValidationWebhookHandler) UpdateExtraConfig(
	_ context.Context,
	extraConfig util.ExtraConfig,
	_ logd.Logger,
) {
	h.extraConfig.Store(&extraConfig)
}

func (h *SignalControlValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/signal-control/validate", handler)

	return nil
}

func (h *SignalControlValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := logd.FromContext(ctx)
	signalControlResource := &dash0v1alpha1.Dash0SignalControl{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, signalControlResource); err != nil {
		logger.Warn("Rejecting invalid Signal Control resource.", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if request.Operation == admissionv1.Create {
		allSignalControlResources := &dash0v1alpha1.Dash0SignalControlList{}
		if err := h.Client.List(ctx, allSignalControlResources); err != nil {
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list all Dash0 Signal Control resources: %w", err))
		}
		if len(allSignalControlResources.Items) > 0 {
			logger.Warn("Rejecting Signal Control resource, another one already exists.",
				"existing", allSignalControlResources.Items[0].Name)
			return admission.Denied(
				fmt.Sprintf("At least one Dash0 Signal Control resource (%s) already exists in this cluster. "+
					"Only one Signal Control resource is allowed per cluster.",
					allSignalControlResources.Items[0].Name,
				))
		}
	}

	if request.Operation == admissionv1.Create || request.Operation == admissionv1.Update {
		if pointers.ReadBoolPointerWithDefault(signalControlResource.Spec.Enabled, true) &&
			!h.hasDash0ExportConfigured(ctx) {
			logger.Warn("Rejecting Signal Control resource, no Dash0 export configured.")
			return admission.Denied(
				"No Dash0 operator configuration with a Dash0 export was found. Signal Control " +
					"requires a Dash0 export with an auth token for the Decision Maker connection. " +
					"Configure a Dash0 export in the operator configuration resource before enabling " +
					"Signal Control.")
		}

		if err := validateOperationProcessorCardinalityRules(
			signalControlResource.Spec.OperationProcessor.CardinalityRules); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid operation processor cardinality rules.",
				"error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"signalToMetrics.cacheExpiration", signalControlResource.Spec.SignalToMetrics.CacheExpiration); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid signalToMetrics.cacheExpiration.", "error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"spamFilter.cacheExpiration", signalControlResource.Spec.SpamFilter.CacheExpiration); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid spamFilter.cacheExpiration.", "error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"edgeProxy.settingsRefreshInterval", signalControlResource.Spec.EdgeProxy.SettingsRefreshInterval); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid edgeProxy.settingsRefreshInterval.", "error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"logEnrichment.patternRefreshInterval", signalControlResource.Spec.LogEnrichment.PatternRefreshInterval); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid logEnrichment.patternRefreshInterval.", "error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"logEnrichment.parserCacheExpiration", signalControlResource.Spec.LogEnrichment.ParserCacheExpiration); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid logEnrichment.parserCacheExpiration.", "error", err)
			return admission.Denied(err.Error())
		}

		if err := validateDurationBounds(
			"logEnrichment.groupingCacheExpiration", signalControlResource.Spec.LogEnrichment.GroupingCacheExpiration); err != nil {
			logger.Warn("Rejecting Signal Control resource, invalid logEnrichment.groupingCacheExpiration.", "error", err)
			return admission.Denied(err.Error())
		}

		unchanged, errorResponse := componentSettingsAreUnchanged(
			request,
			&dash0v1alpha1.Dash0SignalControl{},
			func(oldResource *dash0v1alpha1.Dash0SignalControl) *dash0v1alpha1.SignalControlComponents {
				return oldResource.Spec.Components
			},
			signalControlResource.Spec.Components,
			logger,
		)
		if errorResponse != nil {
			return *errorResponse
		}
		var check util.ComponentSettingsCheck
		if !unchanged {
			check = util.CheckComponentSettings(
				*h.extraConfig.Load(), h.extraConfigDefaults, nil, signalControlResource.Spec.Components)
		}
		// Not only checked when spec.components changes, since a change of spec.sampling can make the limit too small.
		reservoirSettingsUnchanged, errorResponse := componentSettingsAreUnchanged(
			request,
			&dash0v1alpha1.Dash0SignalControl{},
			reservoirStorageSettingsOf,
			reservoirStorageSettingsOf(signalControlResource),
			logger,
		)
		if errorResponse != nil {
			return *errorResponse
		}
		if !reservoirSettingsUnchanged {
			if msg := h.validateReservoirEphemeralStorageLimit(signalControlResource); msg != "" {
				check.Invalid = append(check.Invalid, msg)
			}
		}
		return componentSettingsCheckResponse("Signal Control resource", check, logger)
	}

	return admission.Allowed("")
}

// reservoirStorageSettingsOf returns the part of the spec that validateReservoirEphemeralStorageLimit depends on.
func reservoirStorageSettingsOf(signalControlResource *dash0v1alpha1.Dash0SignalControl) dash0v1alpha1.Dash0SignalControlSpec {
	return dash0v1alpha1.Dash0SignalControlSpec{
		Enabled:    signalControlResource.Spec.Enabled,
		Sampling:   signalControlResource.Spec.Sampling,
		Components: signalControlResource.Spec.Components,
	}
}

// validateReservoirEphemeralStorageLimit rejects an ephemeral-storage limit of the Signal Control collector (from
// spec.components or from the Helm chart) that is below the ephemeral-storage request the operator derives for the
// disk-backed trace reservoir. Without an explicit request, the API server would reject the collector Deployment. With
// one, the pod would be evicted once the reservoir fills up.
func (h *SignalControlValidationWebhookHandler) validateReservoirEphemeralStorageLimit(
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
) string {
	request, usesDiskReservoir := otelcolresources.ReservoirEphemeralStorageRequest(signalControlResource)
	if !usesDiskReservoir {
		return ""
	}
	resources := util.EffectiveExtraConfig(*h.extraConfig.Load(), h.extraConfigDefaults, nil, signalControlResource).
		SignalControlCollectorContainerResources.ToResourceRequirements()
	limit, hasLimit := resources.Limits[corev1.ResourceEphemeralStorage]
	if !hasLimit || limit.Cmp(request) >= 0 {
		return ""
	}
	components := signalControlResource.Spec.Components
	if components == nil || components.Collector == nil || components.Collector.CollectorContainerResources == nil {
		return fmt.Sprintf("the ephemeral-storage limit %s of the Signal Control collector, set via the Helm value "+
			"operator.collectors.signalControlCollectorContainerResources, must be at least %s, the ephemeral-storage "+
			"request derived from spec.sampling.reservoir.maxDiskBytes, since the trace reservoir counts towards the "+
			"limit; raise the limit via Helm or set spec.components.collector.collectorContainerResources",
			limit.String(), request.String())
	}
	return field.Invalid(
		field.NewPath("spec", "components", "collector", "collectorContainerResources", "limits").
			Key(string(corev1.ResourceEphemeralStorage)),
		limit.String(),
		fmt.Sprintf("must be at least %s, the ephemeral-storage request derived from "+
			"spec.sampling.reservoir.maxDiskBytes, since the trace reservoir counts towards the limit", request.String()),
	).Error()
}

// validateOperationProcessorCardinalityRules rejects cardinality rules that the dash0operation processor would
// reject at collector startup. It only covers the checks that cannot be expressed as CRD validation markers: that
// each matcher's regex compiles, and that its capture group count matches the number of replacements. Presence of
// the id, at least one matcher, a non-empty regex and at least one replacement are already enforced via kubebuilder
// markers on the CRD types.
func validateOperationProcessorCardinalityRules(rules []dash0v1alpha1.CardinalityRule) error {
	for i, rule := range rules {
		for j, matcher := range rule.OperationMatchers {
			re, err := regexp.Compile(matcher.Regex)
			if err != nil {
				return fmt.Errorf(
					"operationProcessor.cardinalityRules[%d] (%s) operationMatchers[%d]: invalid regex %q: %w",
					i, rule.Id, j, matcher.Regex, err)
			}
			if numGroups, numReplacements := re.NumSubexp(), len(matcher.Replacements); numGroups != numReplacements {
				return fmt.Errorf(
					"operationProcessor.cardinalityRules[%d] (%s) operationMatchers[%d]: regex has %d capture "+
						"group(s) but %d replacement(s) provided",
					i, rule.Id, j, numGroups, numReplacements)
			}
		}
	}
	return nil
}

// validateDurationBounds rejects a non-zero duration outside the 10s–1h range shared by the settings-driven Signal
// Control knobs (signalToMetrics.cacheExpiration, spamFilter.cacheExpiration and edgeProxy.settingsRefreshInterval).
// A nil or zero value is accepted: the collector config and the Edge Proxy builder omit a zero value from the rendered
// config, so the component keeps its factory default (60s) instead of validating a literal 0.
func validateDurationBounds(fieldPath string, d *metav1.Duration) error {
	if d == nil || d.Duration == 0 {
		return nil
	}
	if d.Duration < durationBoundMin || d.Duration > durationBoundMax {
		return fmt.Errorf("%s must be between 10s and 1h, got %s", fieldPath, d.Duration)
	}
	return nil
}

func (h *SignalControlValidationWebhookHandler) hasDash0ExportConfigured(ctx context.Context) bool {
	allOperatorConfigs := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := h.Client.List(ctx, allOperatorConfigs); err != nil {
		return false
	}
	for _, config := range allOperatorConfigs.Items {
		for _, export := range config.EffectiveExports() {
			if export.Dash0 != nil {
				return true
			}
		}
	}
	return false
}
