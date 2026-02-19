// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type normalizeOperatorConfigurationResourceSpecTestConfig struct {
	spec   dash0v1alpha1.Dash0OperatorConfigurationSpec
	wanted dash0v1alpha1.Dash0OperatorConfigurationSpec
}

var _ = Describe("The mutating webhook for the operator configuration resource", func() {

	Describe("when a new operator configuration resource is created", Ordered, func() {
		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should set defaults", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "dash0-operator-configuration-test-1",
					},
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
				spec := operatorConfigurationResource.Spec
				g.Expect(spec.SelfMonitoring.Enabled).To(Equal(ptr.To(true)))
				g.Expect(spec.KubernetesInfrastructureMetricsCollection.Enabled).To(Equal(ptr.To(true)))
				//nolint:staticcheck
				g.Expect(spec.KubernetesInfrastructureMetricsCollectionEnabled).To(Equal(ptr.To(true)))
				g.Expect(spec.CollectPodLabelsAndAnnotations.Enabled).To(Equal(ptr.To(true)))
				g.Expect(spec.CollectNamespaceLabelsAndAnnotations.Enabled).To(Equal(ptr.To(false)))
				g.Expect(spec.TelemetryCollection.Enabled).To(Equal(ptr.To(true)))
			})
		})

		It("should set defaults if telemetry collection is disabled", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "dash0-operator-configuration-test-1",
					},
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						TelemetryCollection: dash0v1alpha1.TelemetryCollection{
							Enabled: ptr.To(false),
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
				spec := operatorConfigurationResource.Spec
				g.Expect(spec.SelfMonitoring.Enabled).To(Equal(ptr.To(false)))
				g.Expect(spec.KubernetesInfrastructureMetricsCollection.Enabled).To(Equal(ptr.To(false)))
				//nolint:staticcheck
				g.Expect(spec.KubernetesInfrastructureMetricsCollectionEnabled).To(Equal(ptr.To(false)))
				g.Expect(spec.CollectPodLabelsAndAnnotations.Enabled).To(Equal(ptr.To(false)))
				g.Expect(spec.CollectNamespaceLabelsAndAnnotations.Enabled).To(Equal(ptr.To(false)))
				g.Expect(spec.TelemetryCollection.Enabled).To(Equal(ptr.To(false)))
			})
		})
	})

	DescribeTable("should normalize the resource spec", func(testConfig normalizeOperatorConfigurationResourceSpecTestConfig) {
		spec := testConfig.spec
		operatorConfigurationMutatingWebhookHandler.normalizeOperatorConfigurationResourceSpec(&spec)
		Expect(spec).To(Equal(testConfig.wanted))
	}, Entry("given an empty spec, set all default values",
		normalizeOperatorConfigurationResourceSpecTestConfig{
			spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
				SelfMonitoring: dash0v1alpha1.SelfMonitoring{
					Enabled: ptr.To(true),
				},
				KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
					Enabled: ptr.To(true),
				},
				KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
				CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
					Enabled: ptr.To(true),
				},
				CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
					Enabled: ptr.To(false),
				},
				PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
					Enabled: ptr.To(false),
				},
				TelemetryCollection: dash0v1alpha1.TelemetryCollection{
					Enabled: ptr.To(true),
				},
			},
		}),
		Entry("given empty structs, set all default values",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				// This is actully the same test case as before, given how the default values for a structs work in Go.
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{},
					CollectPodLabelsAndAnnotations:            dash0v1alpha1.CollectPodLabelsAndAnnotations{},
					TelemetryCollection:                       dash0v1alpha1.TelemetryCollection{},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("given telemetry collection disabled, set most other flags to false",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						// self-monitoring does not depend on the telemetry collection flag
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			}),
		Entry("given telemetry collection disabled & empty structs, set most other flags to false",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{},
					CollectPodLabelsAndAnnotations:            dash0v1alpha1.CollectPodLabelsAndAnnotations{},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			}),
		Entry("given telemetry collection absent & individual flags disabled, set telemetry collection to true and leave other flags as provided",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("given telemetry collection enabled explicitly & individual flags disabled, leave everything as provided",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(false),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("given telemetry collection enabled implicitly & individual flags enabled, enable telemetry collection",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(true),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("given telemetry collection disabled & individual flags enabled, do nothing",
			// This is an invalid combination, but the validation is not handled in
			// normalizeOperatorConfigurationResourceSpecTestConfig but the validation webhook, so here we are just
			// testing that the normalization does not change the spec.
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(false),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			}),

		Entry("should migrate deprecated export to exports when only export is set",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export:  nil,
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("should not modify exports when only exports is set",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export:  nil,
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
		Entry("should not clear export when both export and exports are set (validation webhook will reject)",
			normalizeOperatorConfigurationResourceSpecTestConfig{
				spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export:  Dash0ExportWithEndpointAndToken(),
					Exports: []dash0common.Export{*GrpcExportTest()},
				},
				wanted: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					// export is NOT cleared, both fields remain as-is
					Export:  Dash0ExportWithEndpointAndToken(),
					Exports: []dash0common.Export{*GrpcExportTest()},
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: ptr.To(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: ptr.To(false),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(true),
					},
				},
			}),
	)
})
