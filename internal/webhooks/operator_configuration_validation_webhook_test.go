// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The validation webhook for the operator configuration resource", func() {

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should reject a new operator configuration resources if there already is one in the cluster", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dash0-operator-configuration-test-1",
				},
				Spec: OperatorConfigurationResourceDefaultSpec,
			})
		Expect(err).ToNot(HaveOccurred())

		_, err = CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dash0-operator-configuration-test-2",
				},
				Spec: OperatorConfigurationResourceDefaultSpec,
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: At least one Dash0 " +
				"operator configuration resource (dash0-operator-configuration-test-1) already exists in this " +
				"cluster. Only one operator configuration resource is allowed per cluster.")))
	})

	It("should reject a new operator configuration resource without spec (and thus without export) since self-monitoring defaults to true", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for " +
				"self-monitoring telemetry.")))
	})

	It("should reject a new operator configuration resource without export if self-monitoring is unset and defaults to true", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for " +
				"self-monitoring telemetry.")))
	})

	It("should reject a new operator configuration resource without export if self-monitoring is enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
				"export configuration. Either disable self-monitoring or provide an export configuration for " +
				"self-monitoring telemetry.")))
	})

	It("should allow a new operator configuration resource without export if self-monitoring is disabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
				},
			})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow a new operator configuration resource with self-monitoring enabled if it has an export", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec:       OperatorConfigurationResourceDefaultSpec,
			})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but Kubernetes infra metrics collection explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: ptr.To(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has Kubernetes infrastructure metrics collection explicitly " +
				"enabled, although telemetry collection is disabled. This is an invalid combination. Please either " +
				"set telemetryCollection.enabled=true or " +
				"kubernetesInfrastructureMetricsCollection.enabled=false.")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but Kubernetes infra metrics collection explicitly enabled (via legacy setting)", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					KubernetesInfrastructureMetricsCollectionEnabled: ptr.To(true),
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has Kubernetes infrastructure metrics collection explicitly " +
				"enabled (via the deprecated legacy setting kubernetesInfrastructureMetricsCollectionEnabled), " +
				"although telemetry collection is disabled. This is an invalid combination. Please either set " +
				"telemetryCollection.enabled=true or " +
				"kubernetesInfrastructureMetricsCollection.enabled=false.")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but label collection explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: ptr.To(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has pod label and annotation collection explicitly " +
				"enabled, although telemetry collection is disabled. This is an invalid combination. Please either " +
				"set telemetryCollection.enabled=true or " +
				"collectPodLabelsAndAnnotations.enabled=false.")))
	})

	It("should allow updating an existing operator configuration resource", func() {
		operatorConfiguration, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
				},
			})
		Expect(err).ToNot(HaveOccurred())

		operatorConfiguration.Spec.SelfMonitoring.Enabled = ptr.To(true)
		operatorConfiguration.Spec.Export = &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		}

		err = k8sClient.Update(ctx, operatorConfiguration)
		Expect(err).ToNot(HaveOccurred())
	})
})
