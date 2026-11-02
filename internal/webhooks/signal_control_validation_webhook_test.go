// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

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

var _ = Describe("validateDurationBounds", func() {

	It("should accept a nil duration", func() {
		Expect(validateDurationBounds("signalToMetrics.cacheExpiration", nil)).To(Succeed())
	})

	It("should accept a zero duration", func() {
		Expect(validateDurationBounds(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 0})).To(Succeed())
	})

	It("should accept the lower bound of 10s", func() {
		Expect(validateDurationBounds(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 10 * time.Second})).To(Succeed())
	})

	It("should accept the upper bound of 1h", func() {
		Expect(validateDurationBounds(
			"spamFilter.cacheExpiration", &metav1.Duration{Duration: time.Hour})).To(Succeed())
	})

	It("should reject a value below the lower bound", func() {
		err := validateDurationBounds(
			"signalToMetrics.cacheExpiration", &metav1.Duration{Duration: 5 * time.Second})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("signalToMetrics.cacheExpiration"))
		Expect(err.Error()).To(ContainSubstring("must be between 10s and 1h"))
	})

	It("should reject a value above the upper bound", func() {
		err := validateDurationBounds(
			"spamFilter.cacheExpiration", &metav1.Duration{Duration: 2 * time.Hour})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spamFilter.cacheExpiration"))
		Expect(err.Error()).To(ContainSubstring("must be between 10s and 1h"))
	})
})

var _ = Describe("The Signal Control validation webhook (cacheExpiration bounds)", func() {

	var handler *SignalControlValidationWebhookHandler

	BeforeEach(func() {
		handler = NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
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

	It("should deny a resource with an out-of-range edgeProxy.settingsRefreshInterval", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			EdgeProxy: dash0v1alpha1.EdgeProxyConfig{
				SettingsRefreshInterval: &metav1.Duration{Duration: 2 * time.Hour},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("edgeProxy.settingsRefreshInterval must be between 10s and 1h"))
	})

	It("should deny a resource with an out-of-range logEnrichment.patternRefreshInterval", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			LogEnrichment: dash0v1alpha1.LogEnrichmentConfig{
				PatternRefreshInterval: &metav1.Duration{Duration: 5 * time.Second},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("logEnrichment.patternRefreshInterval must be between 10s and 1h"))
	})

	It("should deny a resource with an out-of-range logEnrichment.parserCacheExpiration", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			LogEnrichment: dash0v1alpha1.LogEnrichmentConfig{
				ParserCacheExpiration: &metav1.Duration{Duration: 2 * time.Hour},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("logEnrichment.parserCacheExpiration must be between 10s and 1h"))
	})

	It("should deny a resource with an out-of-range logEnrichment.groupingCacheExpiration", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			LogEnrichment: dash0v1alpha1.LogEnrichmentConfig{
				GroupingCacheExpiration: &metav1.Duration{Duration: 2 * time.Hour},
			},
		})
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(
			ContainSubstring("logEnrichment.groupingCacheExpiration must be between 10s and 1h"))
	})

	It("should allow a resource with in-range cacheExpiration and settingsRefreshInterval values", func() {
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			SignalToMetrics: dash0v1alpha1.SignalToMetricsConfig{
				CacheExpiration: &metav1.Duration{Duration: 30 * time.Second},
			},
			SpamFilter: dash0v1alpha1.SpamFilterConfig{
				CacheExpiration: &metav1.Duration{Duration: 5 * time.Minute},
			},
			EdgeProxy: dash0v1alpha1.EdgeProxyConfig{
				SettingsRefreshInterval: &metav1.Duration{Duration: 45 * time.Second},
			},
			LogEnrichment: dash0v1alpha1.LogEnrichmentConfig{
				PatternRefreshInterval:  &metav1.Duration{Duration: 30 * time.Second},
				ParserCacheExpiration:   &metav1.Duration{Duration: 1 * time.Minute},
				GroupingCacheExpiration: &metav1.Duration{Duration: 1 * time.Minute},
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
		handler = NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
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

var _ = Describe("The Signal Control validation webhook (component settings)", func() {

	componentsSpec := dash0v1alpha1.Dash0SignalControlSpec{
		Components: &dash0v1alpha1.SignalControlComponents{
			EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
				Replicas: new(int32(3)),
			},
		},
	}

	BeforeEach(func() {
		createOperatorConfigurationWithExport(*Dash0ExportWithEndpointAndToken())
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should not return warnings when the Helm chart uses the default values", func() {
		handler := NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
		response := handler.Handle(ctx, signalControlAdmissionRequest(componentsSpec))
		Expect(response.Allowed).To(BeTrue())
		Expect(response.Warnings).To(BeEmpty())
	})

	It("should return a warning for a setting that overrides a different value provided via the Helm chart", func() {
		extraConfig := util.ExtraConfigDefaults
		extraConfig.EdgeProxyReplicas = 4
		handler := NewSignalControlValidationWebhookHandler(k8sClient, extraConfig, util.ExtraConfigDefaults)
		response := handler.Handle(ctx, signalControlAdmissionRequest(componentsSpec))
		Expect(response.Allowed).To(BeTrue())
		Expect(response.Warnings).To(ConsistOf(
			"spec.components.edgeProxy.replicas overrides the custom Helm value " +
				"operator.signalControl.edgeProxy.replicas: 3 instead of 4"))
	})

	It("should reject a resource request that is greater than the limit", func() {
		handler := NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
		request := signalControlAdmissionRequest(dash0v1alpha1.Dash0SignalControlSpec{
			Components: &dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
						},
					},
				},
			},
		})
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			request.Operation = operation
			response := handler.Handle(ctx, request)
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(Equal(
				"The provided Signal Control resource has invalid component settings: " +
					"spec.components.edgeProxy.containerResources.requests[cpu]: Invalid value: \"200m\": must be less " +
					"than or equal to the cpu limit of 100m."))
		}
	})

	It("should allow an update that leaves invalid component settings unchanged", func() {
		handler := NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
		spec := dash0v1alpha1.Dash0SignalControlSpec{
			Components: &dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"in valid": "value"}},
				},
			},
		}
		request := signalControlAdmissionRequest(spec)
		request.Operation = admissionv1.Update
		request.OldObject = signalControlAdmissionRequest(spec).Object
		response := handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeTrue())

		request.OldObject = signalControlAdmissionRequest(componentsSpec).Object
		response = handler.Handle(ctx, request)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Message).To(ContainSubstring(
			"spec.components.edgeProxy.labels: Invalid value: \"in valid\""))
	})

	Describe("ephemeral-storage limit and disk-backed trace reservoir", func() {
		withReservoir := func(
			reservoirType dash0v1alpha1.ReservoirType,
			resources *dash0v1alpha1.ContainerResources,
		) dash0v1alpha1.Dash0SignalControlSpec {
			spec := dash0v1alpha1.Dash0SignalControlSpec{
				Sampling: dash0v1alpha1.SamplingConfig{
					Reservoir: &dash0v1alpha1.ReservoirConfig{
						Type:         &reservoirType,
						MaxDiskBytes: new(resource.MustParse("1Gi")),
					},
				},
			}
			if resources != nil {
				spec.Components = &dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{
						CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
							ContainerResources: *resources,
						},
					},
				}
			}
			return spec
		}
		ephemeralStorageLimit := func(limit string) *dash0v1alpha1.ContainerResources {
			return &dash0v1alpha1.ContainerResources{
				Limits: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(limit)},
			}
		}
		const expectedMessage = "The provided Signal Control resource has invalid component settings: " +
			"spec.components.collector.collectorContainerResources.limits[ephemeral-storage]: Invalid value: \"1Gi\": " +
			"must be at least 1280Mi, the ephemeral-storage request derived from spec.sampling.reservoir.maxDiskBytes, " +
			"since the trace reservoir counts towards the limit."

		var handler *SignalControlValidationWebhookHandler

		BeforeEach(func() {
			handler = NewSignalControlValidationWebhookHandler(k8sClient, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
		})

		It("should reject a limit below the derived request", func() {
			response := handler.Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, ephemeralStorageLimit("1Gi"))))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(Equal(expectedMessage))
		})

		It("should reject an update that only switches to the disk-backed reservoir", func() {
			request := signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, ephemeralStorageLimit("1Gi")))
			request.Operation = admissionv1.Update
			request.OldObject = signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeSerializedMemory, ephemeralStorageLimit("1Gi"))).Object
			response := handler.Handle(ctx, request)
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(Equal(expectedMessage))
		})

		It("should allow an update that leaves the reservoir and component settings unchanged", func() {
			spec := withReservoir(dash0v1alpha1.ReservoirTypeDisk, ephemeralStorageLimit("1Gi"))
			request := signalControlAdmissionRequest(spec)
			request.Operation = admissionv1.Update
			request.OldObject = signalControlAdmissionRequest(spec).Object
			Expect(handler.Handle(ctx, request).Allowed).To(BeTrue())
		})

		It("should allow a limit that is not below the derived request", func() {
			response := handler.Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, ephemeralStorageLimit("1280Mi"))))
			Expect(response.Allowed).To(BeTrue())
		})

		It("should reject a limit below the derived request with an explicit request", func() {
			resources := ephemeralStorageLimit("1Gi")
			resources.Requests = corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("512Mi")}
			response := handler.Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, resources)))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(Equal(expectedMessage))
		})

		It("should allow a limit below the derived request without the disk-backed reservoir", func() {
			response := handler.Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeSerializedMemory, ephemeralStorageLimit("1Gi"))))
			Expect(response.Allowed).To(BeTrue())
		})

		withHelmLimit := func(limit string) *SignalControlValidationWebhookHandler {
			extraConfig := util.ExtraConfigDefaults
			extraConfig.SignalControlCollectorContainerResources.Limits = corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse(limit),
			}
			return NewSignalControlValidationWebhookHandler(k8sClient, extraConfig, util.ExtraConfigDefaults)
		}

		It("should reject a limit provided via the Helm chart below the derived request", func() {
			response := withHelmLimit("1Gi").Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, nil)))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(Equal(
				"The provided Signal Control resource has invalid component settings: the ephemeral-storage limit 1Gi " +
					"of the Signal Control collector, set via the Helm value " +
					"operator.collectors.signalControlCollectorContainerResources, must be at least 1280Mi, the " +
					"ephemeral-storage request derived from spec.sampling.reservoir.maxDiskBytes, since the trace " +
					"reservoir counts towards the limit; raise the limit via Helm or set " +
					"spec.components.collector.collectorContainerResources."))
		})

		It("should reject a Helm limit if spec.components only sets other collector settings", func() {
			spec := withReservoir(dash0v1alpha1.ReservoirTypeDisk, nil)
			spec.Components = &dash0v1alpha1.SignalControlComponents{
				Collector: &dash0v1alpha1.SignalControlCollectorSettings{Replicas: new(int32(2))},
			}
			response := withHelmLimit("1Gi").Handle(ctx, signalControlAdmissionRequest(spec))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring(
				"set via the Helm value operator.collectors.signalControlCollectorContainerResources"))
		})

		It("should allow a limit provided via the Helm chart that is not below the derived request", func() {
			response := withHelmLimit("1280Mi").Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, nil)))
			Expect(response.Allowed).To(BeTrue())
		})

		It("should allow a limit provided via the Helm chart that spec.components replaces", func() {
			response := withHelmLimit("1Gi").Handle(ctx, signalControlAdmissionRequest(
				withReservoir(dash0v1alpha1.ReservoirTypeDisk, &dash0v1alpha1.ContainerResources{})))
			Expect(response.Allowed).To(BeTrue())
		})
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
