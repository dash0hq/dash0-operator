// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The validation webhook for the operator configuration resource", func() {

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should reject a new operator configuration resource if there already is one in the cluster", func() {
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
						Enabled: new(true),
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
						Enabled: new(false),
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
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
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
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					KubernetesInfrastructureMetricsCollectionEnabled: new(true),
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
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
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
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

	It("should reject a new operator configuration resource with telemetry collection disabled but namespace label collection explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has namespace label and annotation collection explicitly " +
				"enabled, although telemetry collection is disabled. This is an invalid combination. Please either " +
				"set telemetryCollection.enabled=true or " +
				"collectNamespaceLabelsAndAnnotations.enabled=false.")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but node label collection explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					CollectNodeLabelsAndAnnotations: dash0v1alpha1.CollectNodeLabelsAndAnnotations{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
				"Dash0 operator configuration resource has node label and annotation collection explicitly " +
				"enabled, although telemetry collection is disabled. This is an invalid combination. Please either " +
				"set telemetryCollection.enabled=true or " +
				"collectNodeLabelsAndAnnotations.enabled=false.")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but Prometheus CRD support explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(ErrorMessageOperatorConfigurationPrometheusCrdSupportInvalid)))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but profiling explicitly enabled", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Profiling: &dash0v1alpha1.Profiling{
						Enabled: new(true),
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"profiling explicitly enabled, although telemetry collection is disabled")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but filters set", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Filter: &dash0common.Filter{
						Traces: &dash0common.TraceFilter{
							SpanFilter: []string{`attributes["http.route"] == "/ready"`},
						},
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"telemetry filters, although telemetry collection is disabled")))
	})

	It("should reject a new operator configuration resource with telemetry collection disabled but transformations set", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Transform: &dash0common.Transform{
						Traces: []json.RawMessage{
							json.RawMessage(`"truncate_all(span.attributes, 128)"`),
						},
					},
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"telemetry transformations, although telemetry collection is disabled")))
	})

	It("should reject a new operator configuration resource with an invalid filter condition", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Filter: &dash0common.Filter{
						Traces: &dash0common.TraceFilter{
							SpanFilter: []string{`NoSuchFunction(attributes["http.route"])`},
						},
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(`undefined function "NoSuchFunction"`)))
	})

	It("should reject a new operator configuration resource with an invalid transform statement", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Transform: &dash0common.Transform{
						Traces: []json.RawMessage{
							json.RawMessage(`"no_such_function(span.attributes, 128)"`),
						},
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			`inferred context "span" does not support the function "no_such_function"`)))
	})

	It("should allow a new operator configuration resource with valid filters and transformations", func() {
		operatorConfigurationResource, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Filter: &dash0common.Filter{
						Traces: &dash0common.TraceFilter{
							SpanFilter: []string{`attributes["http.route"] == "/ready"`},
						},
					},
					Transform: &dash0common.Transform{
						Traces: []json.RawMessage{
							json.RawMessage(`"truncate_all(span.attributes, 128)"`),
						},
					},
				},
			})
		Expect(err).ToNot(HaveOccurred())
		Expect(operatorConfigurationResource).ToNot(BeNil())
	})

	It("should reject a new operator configuration resource with a GRPC export having both insecure and insecureSkipVerify set to true", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{
						{
							Grpc: &dash0common.GrpcConfiguration{
								Endpoint:           EndpointGrpcTest,
								Insecure:           new(true),
								InsecureSkipVerify: new(true),
							},
						},
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(ErrorMessageOperatorConfigurationGrpcExportInvalidInsecure)))
	})

	It("should allow updating an existing operator configuration resource", func() {
		operatorConfiguration, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: new(false),
					},
				},
			})
		Expect(err).ToNot(HaveOccurred())

		operatorConfiguration.Spec.SelfMonitoring.Enabled = new(true)
		operatorConfiguration.Spec.Exports = []dash0common.Export{
			{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0common.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		}

		err = k8sClient.Update(ctx, operatorConfiguration)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow a new operator configuration resource with only exports set", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: new(false),
					},
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				},
			})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow a new operator configuration resource with only export set (mutating webhook migrates it)", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: new(false),
					},
					Export: Dash0ExportWithEndpointAndToken(),
				},
			})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should reject a monitoring template with exports", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					MonitoringTemplate: &dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
								Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
							},
							LogCollection:      dash0common.LogCollection{Enabled: new(true)},
							EventCollection:    dash0common.EventCollection{Enabled: new(true)},
							PrometheusScraping: dash0common.PrometheusScraping{Enabled: new(true)},
						},
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided Dash0 " +
				"operator configuration resource has a monitoring template with `exports`. Please use the `exports` field in " +
				"the operator configuration and remove the `exports` from the monitoringTemplate.spec.")))
	})

	It("should reject a monitoring template with legacy export", func() {
		_, err := CreateOperatorConfigurationResource(
			ctx,
			k8sClient,
			&dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					MonitoringTemplate: &dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: Dash0ExportWithEndpointAndToken(),
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
								Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
							},
							LogCollection:      dash0common.LogCollection{Enabled: new(true)},
							EventCollection:    dash0common.EventCollection{Enabled: new(true)},
							PrometheusScraping: dash0common.PrometheusScraping{Enabled: new(true)},
						},
					},
				},
			})
		Expect(err).To(MatchError(ContainSubstring(
			"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided Dash0 " +
				"operator configuration resource has a monitoring template with `export`. Please use the `exports` field in " +
				"the operator configuration and remove the `export` from the monitoringTemplate.spec.")))
	})

	Context("when Signal Control is enabled", func() {
		const signalControlName = "dash0-signal-control-test"

		createSignalControl := func(enabled *bool) {
			Expect(k8sClient.Create(ctx, &dash0v1alpha1.Dash0SignalControl{
				ObjectMeta: metav1.ObjectMeta{Name: signalControlName},
				Spec:       dash0v1alpha1.Dash0SignalControlSpec{Enabled: enabled},
			})).To(Succeed())
		}

		AfterEach(func() {
			signalControl := &dash0v1alpha1.Dash0SignalControl{ObjectMeta: metav1.ObjectMeta{Name: signalControlName}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, signalControl))).To(Succeed())
		})

		It("should reject creating an operator configuration resource without a Dash0 export", func() {
			createSignalControl(new(true))
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				ErrorMessageOperatorConfigurationDash0ExportRequiredBySignalControl)))
		})

		It("should reject creating an operator configuration resource with only a non-Dash0 export", func() {
			createSignalControl(new(true))
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
						Exports:        []dash0common.Export{*HttpExportTest()},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				ErrorMessageOperatorConfigurationDash0ExportRequiredBySignalControl)))
		})

		It("should reject removing the Dash0 export from an existing operator configuration resource", func() {
			createSignalControl(new(true))
			operatorConfiguration, err := CreateOperatorConfigurationResource(
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

			operatorConfiguration.Spec.Exports = nil
			err = k8sClient.Update(ctx, operatorConfiguration)
			Expect(err).To(MatchError(ContainSubstring(
				ErrorMessageOperatorConfigurationDash0ExportRequiredBySignalControl)))
		})

		It("should allow an operator configuration resource that keeps a Dash0 export", func() {
			createSignalControl(new(true))
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow an operator configuration resource without a Dash0 export when Signal Control is disabled", func() {
			createSignalControl(new(false))
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("with telemetry collection disabled via Helm", Ordered, func() {
		BeforeAll(func() {
			operatorConfigurationValidationWebhookHandler.telemetryCollectionEnabledViaHelm = false
		})

		AfterAll(func() {
			operatorConfigurationValidationWebhookHandler.telemetryCollectionEnabledViaHelm = true
		})

		It("should reject enabling telemetry collection via the operator configuration resource", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						TelemetryCollection: dash0v1alpha1.TelemetryCollection{
							Enabled: new(true),
						},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: Telemetry collection " +
					"has been disabled via the Helm chart (operator.telemetryCollectionEnabled: false), but the provided Dash0 " +
					"operator configuration resource has telemetryCollection.enabled=true. Telemetry collection cannot be " +
					"enabled via the operator configuration resource when it has been disabled via the Helm chart. Instead, " +
					"run helm upgrade --install to set operator.telemetryCollectionEnabled: true via the Helm chart.")))
		})

		It("should allow updates when operator configuration resource has telemetry collection disabled", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						TelemetryCollection: dash0v1alpha1.TelemetryCollection{
							Enabled: new(false),
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("agent0-connector", func() {
		It("should reject enabling the agent0-connector when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						Agent0Connector: dash0v1alpha1.Agent0Connector{
							Enabled: new(true),
						},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The agent0-connector " +
					"has been disabled via the Helm chart (operator.agent0Connector.enabled: false), but the provided Dash0 " +
					"operator configuration resource has agent0Connector.enabled=true. The agent0-connector cannot be " +
					"enabled via the operator configuration resource when it has been disabled via the Helm chart. Instead, " +
					"run helm upgrade --install to set operator.agent0Connector.enabled: true via the Helm chart.")))
		})

		It("should allow disabling the agent0-connector when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						Agent0Connector: dash0v1alpha1.Agent0Connector{
							Enabled: new(false),
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject an update that enables the agent0-connector when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfigurationResource.Spec.Agent0Connector.Enabled = new(true)

			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(
				MatchError(ContainSubstring(ErrorMessageAgent0ConnectorDisabledViaHelm)))
		})

		It("should allow an unrelated update when the agent0-connector has been disabled via Helm after the fact", func() {
			// Store agent0Connector.enabled=true while the agent0-connector is still enabled via Helm, ...
			operatorConfigurationValidationWebhookHandler.agent0ConnectorEnabledViaHelm = true
			DeferCleanup(func() {
				operatorConfigurationValidationWebhookHandler.agent0ConnectorEnabledViaHelm = false
			})
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						Agent0Connector: dash0v1alpha1.Agent0Connector{
							Enabled: new(true),
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			// ... then disable it via Helm and update an unrelated field.
			operatorConfigurationValidationWebhookHandler.agent0ConnectorEnabledViaHelm = false
			operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfigurationResource.Spec.ClusterName = "cluster-name-set-after-the-fact"

			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
		})

		Describe("with the agent0-connector enabled via Helm", Ordered, func() {
			BeforeAll(func() {
				operatorConfigurationValidationWebhookHandler.agent0ConnectorEnabledViaHelm = true
			})

			AfterAll(func() {
				operatorConfigurationValidationWebhookHandler.agent0ConnectorEnabledViaHelm = false
			})

			It("should allow enabling the agent0-connector", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							Agent0Connector: dash0v1alpha1.Agent0Connector{
								Enabled: new(true),
							},
						},
					})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should allow disabling the agent0-connector", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							Agent0Connector: dash0v1alpha1.Agent0Connector{
								Enabled: new(false),
							},
						},
					})
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("component settings", func() {
		componentsSpec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
			Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
			Components: &dash0v1alpha1.OperatorConfigurationComponents{
				Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{
					MaxConcurrentCommands: new(int32(4)),
				},
			},
		}

		It("should not return warnings when the Helm chart uses the default values", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(componentsSpec))
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Warnings).To(BeEmpty())
		})

		It("should return a warning for a setting that overrides a different value provided via the Helm chart", func() {
			extraConfig := util.ExtraConfigDefaults
			extraConfig.Agent0ConnectorMaxConcurrentCommands = 3
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, extraConfig, util.ExtraConfigDefaults)
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(componentsSpec))
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Warnings).To(ConsistOf(
				"spec.components.agent0Connector.maxConcurrentCommands overrides the custom Helm value " +
					"operator.agent0Connector.maxConcurrentCommands: 4 instead of 3"))
		})

		It("should use the extra configuration from the latest update", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			extraConfig := util.ExtraConfigDefaults
			extraConfig.Agent0ConnectorMaxConcurrentCommands = 3
			handler.UpdateExtraConfig(ctx, extraConfig, logd.FromContext(ctx))
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(componentsSpec))
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Warnings).To(HaveLen(1))
		})

		It("should reject a resource request that is greater than the limit", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			spec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				Components: &dash0v1alpha1.OperatorConfigurationComponents{
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
						Resources: &dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
						},
					},
				},
			}
			expectedMessage := "The provided Dash0 operator configuration resource has invalid component settings: " +
				"spec.components.instrumentationInitContainer.resources.requests[memory]: Invalid value: \"128Mi\": " +
				"must be less than or equal to the memory limit of 64Mi."
			request := operatorConfigurationAdmissionRequest(spec)
			for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
				request.Operation = operation
				response := handler.Handle(ctx, request)
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(Equal(expectedMessage))
			}
		})

		It("should reject allowed kubectl commands that disable every command", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			spec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				Components: &dash0v1alpha1.OperatorConfigurationComponents{
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{
						AllowedKubectlCommands: &dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{
							ApiResources: new(false),
							ApiVersions:  new(false),
							Auth:         new(false),
							ClusterInfo:  new(false),
							Explain:      new(false),
							Get:          new(false),
							Top:          new(false),
							Version:      new(false),
						},
					},
				},
			}
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(spec))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring(
				"spec.components.agent0Connector.allowedKubectlCommands: Forbidden: at least one kubectl command"))
		})

		Describe("updates that leave spec.components unchanged", func() {
			// A GOMEMLIMIT that is not below the memory limit, e.g. in a resource stored without the validation webhook.
			invalidSpec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
				Components: &dash0v1alpha1.OperatorConfigurationComponents{
					DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
						CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
							ContainerResources: dash0v1alpha1.ContainerResources{
								Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
							},
							GoMemLimit: "600MiB",
						},
					},
				},
			}

			updateRequest := func(
				oldSpec dash0v1alpha1.Dash0OperatorConfigurationSpec,
				spec dash0v1alpha1.Dash0OperatorConfigurationSpec,
			) admission.Request {
				request := operatorConfigurationAdmissionRequest(spec)
				request.Operation = admissionv1.Update
				request.OldObject = operatorConfigurationAdmissionRequest(oldSpec).Object
				return request
			}

			It("should allow an update of other fields without checking spec.components again", func() {
				extraConfig := util.ExtraConfigDefaults
				extraConfig.Agent0ConnectorMaxConcurrentCommands = 3
				handler := NewOperatorConfigurationValidationWebhookHandler(
					k8sClient, true, false, false, extraConfig, util.ExtraConfigDefaults)
				spec := *invalidSpec.DeepCopy()
				spec.Components.Agent0Connector = &dash0v1alpha1.Agent0ConnectorSettings{MaxConcurrentCommands: new(int32(4))}
				updatedSpec := *spec.DeepCopy()
				updatedSpec.ClusterName = "updated-cluster-name"
				response := handler.Handle(ctx, updateRequest(spec, updatedSpec))
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Warnings).To(BeEmpty())
			})

			It("should reject an update that changes spec.components while a setting is invalid", func() {
				handler := NewOperatorConfigurationValidationWebhookHandler(
					k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
				updatedSpec := *invalidSpec.DeepCopy()
				updatedSpec.Components.Agent0Connector =
					&dash0v1alpha1.Agent0ConnectorSettings{MaxConcurrentCommands: new(int32(4))}
				response := handler.Handle(ctx, updateRequest(invalidSpec, updatedSpec))
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(ContainSubstring(
					"spec.components.daemonSetCollector.collectorContainerResources.gomemlimit: Invalid value: " +
						"\"600MiB\": must be less than the memory limit of 500Mi"))
			})

			It("should reject an update with an OldObject that cannot be decoded", func() {
				handler := NewOperatorConfigurationValidationWebhookHandler(
					k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
				request := updateRequest(invalidSpec, invalidSpec)
				request.OldObject = runtime.RawExtension{Raw: []byte("{")}
				response := handler.Handle(ctx, request)
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
			})
		})

		It("should accept a required node affinity without node selector terms, which removes the default", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Components: &dash0v1alpha1.OperatorConfigurationComponents{
						TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
							WorkloadSettings: dash0v1alpha1.WorkloadSettings{
								NodeAffinity: &corev1.NodeAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
										NodeSelectorTerms: []corev1.NodeSelectorTerm{},
									},
								},
							},
						},
					},
				}))
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Warnings).To(BeEmpty())
		})

		It("should check a GOMEMLIMIT against the default memory limit if the setting does not set one", func() {
			handler := NewOperatorConfigurationValidationWebhookHandler(
				k8sClient, true, false, false, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			response := handler.Handle(ctx, operatorConfigurationAdmissionRequest(
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					Components: &dash0v1alpha1.OperatorConfigurationComponents{
						TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
							ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{GoMemLimit: "600MiB"},
						},
					},
				}))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring(
				"spec.components.targetAllocator.containerResources.gomemlimit: Invalid value: \"600MiB\": must be " +
					"less than the memory limit of 500Mi"))
		})
	})

	Describe("synthetics-worker", func() {
		It("should reject enabling the synthetics-worker when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
							Enabled: new(true),
						},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The " +
					"synthetics-worker has been disabled via the Helm chart (operator.syntheticsWorker.enabled: false), " +
					"but the provided Dash0 operator configuration resource has syntheticsWorker.enabled=true. The " +
					"synthetics-worker cannot be enabled via the operator configuration resource when it has been " +
					"disabled via the Helm chart. Instead, run helm upgrade --install to set " +
					"operator.syntheticsWorker.enabled: true via the Helm chart.")))
		})

		It("should allow disabling the synthetics-worker when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
							Enabled: new(false),
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject an update that enables the synthetics-worker when it is disabled via Helm", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfigurationResource.Spec.SyntheticsWorker.Enabled = new(true)

			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(
				MatchError(ContainSubstring(ErrorMessageSyntheticsWorkerDisabledViaHelm)))
		})

		It("should allow an unrelated update when the synthetics-worker has been disabled via Helm after the fact", func() {
			// Store syntheticsWorker.enabled=true while the synthetics-worker is still enabled via Helm, ...
			operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = true
			DeferCleanup(func() {
				operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = false
			})
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
							Enabled: new(true),
							Instances: []dash0v1alpha1.SyntheticsWorkerInstance{{
								LocationID: "test-location",
							}},
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())

			// ... then disable it via Helm and update an unrelated field.
			operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = false
			operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfigurationResource.Spec.ClusterName = "cluster-name-set-after-the-fact"

			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
		})

		Describe("with the synthetics-worker enabled via Helm", Ordered, func() {
			BeforeAll(func() {
				operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = true
			})

			AfterAll(func() {
				operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = false
			})

			It("should allow enabling the synthetics-worker", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
								Enabled: new(true),
								Instances: []dash0v1alpha1.SyntheticsWorkerInstance{{
									LocationID: "test-location",
								}},
							},
						},
					})
				Expect(err).ToNot(HaveOccurred())
			})

			DescribeTable("should only accept non-negative replica counts", func(replicas int32, expectedMessage string) {
				err := k8sClient.Create(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
							Enabled: new(true),
							Instances: []dash0v1alpha1.SyntheticsWorkerInstance{{
								LocationID: "test-location",
								Replicas:   new(replicas),
							}},
						},
					},
				}, client.DryRunAll)
				if expectedMessage == "" {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
				}
			},
				Entry("-1", int32(-1), "replicas in body should be greater than or equal to 0"),
				Entry("0", int32(0), ""),
				Entry("3", int32(3), ""),
			)

			It("should allow disabling the synthetics-worker", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{
								Enabled: new(false),
							},
						},
					})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject enabling the synthetics-worker without any instances", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports:          []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
							SyntheticsWorker: dash0v1alpha1.SyntheticsWorker{Enabled: new(true)},
						},
					})
				Expect(err).To(MatchError(ContainSubstring(ErrorMessageSyntheticsWorkerEnabledWithoutInstances)))
			})

			It("should reject a resource with an unset enabled flag and no instances, since it defaults to enabled", func() {
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						},
					})
				Expect(err).To(MatchError(ContainSubstring(ErrorMessageSyntheticsWorkerEnabledWithoutInstances)))
			})

			It("should allow an unrelated update to a pre-existing enabled-without-instances resource", func() {
				// Simulates the operator's own auto-created configuration resource, which never sets
				// syntheticsWorker.instances; only the request that first creates this state is rejected.
				operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = false
				_, err := CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					&dash0v1alpha1.Dash0OperatorConfiguration{
						ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
						Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
						},
					})
				Expect(err).ToNot(HaveOccurred())
				operatorConfigurationValidationWebhookHandler.syntheticsWorkerEnabledViaHelm = true

				operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				operatorConfigurationResource.Spec.ClusterName = "cluster-name-set-after-the-fact"

				Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
			})
		})
	})
})

func operatorConfigurationAdmissionRequest(spec dash0v1alpha1.Dash0OperatorConfigurationSpec) admission.Request {
	rawJson, err := json.Marshal(dash0v1alpha1.Dash0OperatorConfiguration{Spec: spec})
	Expect(err).ToNot(HaveOccurred())
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Name:      OperatorConfigurationResourceName,
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: rawJson,
			},
		},
	}
}
