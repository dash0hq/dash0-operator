// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type IntelligentEdgeValidationWebhookHandler struct {
	Client client.Client
}

func NewIntelligentEdgeValidationWebhookHandler(
	k8sClient client.Client,
) *IntelligentEdgeValidationWebhookHandler {
	return &IntelligentEdgeValidationWebhookHandler{
		Client: k8sClient,
	}
}

func (h *IntelligentEdgeValidationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/intelligent-edge/validate", handler)

	return nil
}

func (h *IntelligentEdgeValidationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := logd.FromContext(ctx)
	intelligentEdgeResource := &dash0v1alpha1.Dash0IntelligentEdge{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, intelligentEdgeResource); err != nil {
		logger.Warn("Rejecting invalid intelligent edge resource.", "error", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if request.Operation == admissionv1.Create {
		allIntelligentEdgeResources := &dash0v1alpha1.Dash0IntelligentEdgeList{}
		if err := h.Client.List(ctx, allIntelligentEdgeResources); err != nil {
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list all Dash0 intelligent edge resources: %w", err))
		}
		if len(allIntelligentEdgeResources.Items) > 0 {
			logger.Warn("Rejecting intelligent edge resource, another one already exists.",
				"existing", allIntelligentEdgeResources.Items[0].Name)
			return admission.Denied(
				fmt.Sprintf("At least one Dash0 intelligent edge resource (%s) already exists in this cluster. "+
					"Only one intelligent edge resource is allowed per cluster.",
					allIntelligentEdgeResources.Items[0].Name,
				))
		}
	}

	if request.Operation == admissionv1.Create || request.Operation == admissionv1.Update {
		if !h.hasDash0ExportConfigured(ctx) {
			logger.Warn("Rejecting intelligent edge resource, no Dash0 export configured.")
			return admission.Denied(
				"No Dash0 operator configuration with a Dash0 export was found. Intelligent edge " +
					"requires a Dash0 export with an auth token for the Decision Maker connection. " +
					"Configure a Dash0 export in the operator configuration resource before enabling " +
					"intelligent edge.")
		}

		if err := validateOperationProcessorCardinalityRules(
			intelligentEdgeResource.Spec.OperationProcessor.CardinalityRules); err != nil {
			logger.Warn("Rejecting intelligent edge resource, invalid operation processor cardinality rules.",
				"error", err)
			return admission.Denied(err.Error())
		}
	}

	return admission.Allowed("")
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

func (h *IntelligentEdgeValidationWebhookHandler) hasDash0ExportConfigured(ctx context.Context) bool {
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
