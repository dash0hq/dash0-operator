// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("validateOperationProcessorCardinalityRules", func() {

	It("should accept an empty rule list", func() {
		Expect(validateOperationProcessorCardinalityRules(nil)).To(Succeed())
	})

	It("should accept a rule whose regex compiles and whose capture groups match the replacements", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "warehouse-sites",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z0-9-]+)/", Replacements: []string{"site"}},
					{Regex: "/zones/([a-z]+)/([0-9]+)/", Replacements: []string{"zone", "id"}},
				},
			},
		}
		Expect(validateOperationProcessorCardinalityRules(rules)).To(Succeed())
	})

	It("should reject a rule whose regex does not compile", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "broken-regex",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z", Replacements: []string{"site"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operationProcessor.cardinalityRules[0] (broken-regex) operationMatchers[0]"))
		Expect(err.Error()).To(ContainSubstring("invalid regex"))
	})

	It("should reject a matcher whose capture group count differs from the replacement count", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "count-mismatch",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z]+)/([0-9]+)/", Replacements: []string{"site"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("regex has 2 capture group(s) but 1 replacement(s) provided"))
	})

	It("should report the offending rule and matcher indices across multiple rules", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id:                "ok",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{{Regex: "/a/([0-9]+)", Replacements: []string{"n"}}},
			},
			{
				Id: "bad",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/a/([0-9]+)", Replacements: []string{"n"}},
					{Regex: "/b/([0-9]+)", Replacements: []string{"n", "extra"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operationProcessor.cardinalityRules[1] (bad) operationMatchers[1]"))
	})
})

var _ = Describe("validateCacheExpiration", func() {

	It("should accept a nil duration", func() {
		Expect(validateCacheExpiration("signalToMetrics.cacheExpiration", nil)).To(Succeed())
	})

	It("should accept a zero duration", func() {
		Expect(validateCacheExpiration(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 0})).To(Succeed())
	})

	It("should accept the lower bound of 10s", func() {
		Expect(validateCacheExpiration(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 10 * time.Second})).To(Succeed())
	})

	It("should accept the upper bound of 1h", func() {
		Expect(validateCacheExpiration(
			"spamFilter.cacheExpiration", &metav1.Duration{Duration: time.Hour})).To(Succeed())
	})

	It("should reject a value below the lower bound", func() {
		err := validateCacheExpiration(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 5 * time.Second})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("signalToMetrics.cacheExpiration"))
		Expect(err.Error()).To(ContainSubstring("must be between 10s and 1h"))
	})

	It("should reject a value above the upper bound", func() {
		err := validateCacheExpiration(
			"spamFilter.cacheExpiration", &metav1.Duration{Duration: 2 * time.Hour})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spamFilter.cacheExpiration"))
		Expect(err.Error()).To(ContainSubstring("must be between 10s and 1h"))
	})
})

var _ = Describe("The Signal Control validation webhook (cacheExpiration bounds)", func() {

	var handler *SignalControlValidationWebhookHandler

	BeforeEach(func() {
		handler = NewSignalControlValidationWebhookHandler(k8sClient)
		// The cacheExpiration checks are only reached after hasDash0ExportConfigured passes, so an operator
		// configuration with a Dash0 export must exist; otherwise the resource is denied earlier.
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
					Exports:        []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				},
			})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should deny a resource with an out-of-range signalToMetrics.cacheExpiration", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			SignalToMetrics: dash0v1alpha1.SignalToMetricsConfig{
				CacheExpiration: &metav1.Duration{Duration: 5 * time.Second},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("signalToMetrics.cacheExpiration must be between 10s and 1h"))
	})

	It("should deny a resource with an out-of-range spamFilter.cacheExpiration", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			SpamFilter: dash0v1alpha1.SpamFilterConfig{
				CacheExpiration: &metav1.Duration{Duration: 2 * time.Hour},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("spamFilter.cacheExpiration must be between 10s and 1h"))
	})

	It("should allow a resource with in-range cacheExpiration values", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			SignalToMetrics: dash0v1alpha1.SignalToMetricsConfig{
				CacheExpiration: &metav1.Duration{Duration: 30 * time.Second},
			},
			SpamFilter: dash0v1alpha1.SpamFilterConfig{
				CacheExpiration: &metav1.Duration{Duration: 5 * time.Minute},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeTrue())
	})

	It("should allow a resource with unset cacheExpiration values", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeTrue())
	})
})

var _ = Describe("The Signal Control validation webhook (Dash0 export requirement)", func() {

	var handler *SignalControlValidationWebhookHandler

	BeforeEach(func() {
		handler = NewSignalControlValidationWebhookHandler(k8sClient)
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should deny an enabled resource when no Dash0 export is configured", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(ContainSubstring("No Dash0 operator configuration with a Dash0 export"))
	})

	It("should deny an enabled resource when only a non-Dash0 export is configured", func() {
		createOperatorConfigurationWithExport(*HttpExportTest())
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(ContainSubstring("No Dash0 operator configuration with a Dash0 export"))
	})

	It("should allow a disabled resource even when no Dash0 export is configured", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{Enabled: new(false)})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeTrue())
	})

	It("should allow an enabled resource when a Dash0 export is configured", func() {
		createOperatorConfigurationWithExport(*Dash0ExportWithEndpointAndToken())
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeTrue())
	})
})

func createOperatorConfigurationWithExport(export dash0common.Export) {
	_, err := CreateOperatorConfigurationResource(
		ctx,
		k8sClient,
		&dash0v1alpha1.Dash0OperatorConfiguration{
			ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
			Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
				SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
				Exports:        []dash0common.Export{export},
			},
		})
	Expect(err).ToNot(HaveOccurred())
}

func signalControlAdmissionRequest(spec dash0v1alpha1.Dash0SignalControlSpec) admission.Request {
	rawJson, err := json.Marshal(dash0v1alpha1.Dash0SignalControl{Spec: spec})
	Expect(err).ToNot(HaveOccurred())
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Name:      "dash0-signal-control-test",
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: rawJson,
			},
		},
	}
}
