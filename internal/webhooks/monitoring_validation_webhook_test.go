// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The validation webhook for the monitoring resource", func() {

	type validationTestConfig struct {
		spec          dash0v1beta1.Dash0MonitoringSpec
		expectedError string
	}

	AfterEach(func() {
		Expect(
			k8sClient.DeleteAllOf(ctx, &dash0v1beta1.Dash0Monitoring{}, client.InNamespace(TestNamespaceName)),
		).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
	})

	Describe("when validating", Ordered, func() {

		type kubeSystemTestConfig struct {
			namespace               string
			instrumentWorkloadsMode dash0common.InstrumentWorkloadsMode
			expectRejection         bool
		}

		DescribeTable("should reject deploying a monitoring resources to kube-system unless instrumentWorkloads.mode=none", func(testConfig kubeSystemTestConfig) {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testConfig.namespace,
					Name:      MonitoringResourceName,
				},
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: testConfig.instrumentWorkloadsMode,
					},
					Export: &dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
				},
			})

			if testConfig.expectRejection {
				Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf(
					"admission webhook \"validate-monitoring.dash0.com\" denied the request: Rejecting the deployment "+
						"of Dash0 monitoring resource \"%s\" to the Kubernetes system namespace \"%s\" with "+
						"instrumentWorkloads.mode=%s, use instrumentWorkloads.mode=none instead.",
					MonitoringResourceName,
					testConfig.namespace,
					testConfig.instrumentWorkloadsMode,
				))))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("reject deploying to kube-system with instrumentWorkloads.mode=all", kubeSystemTestConfig{
				namespace:               "kube-system",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
				expectRejection:         true,
			}),
			Entry("reject deploying to kube-system with instrumentWorkloads.mode=created-and-updated", kubeSystemTestConfig{
				namespace:               "kube-system",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				expectRejection:         true,
			}),
			Entry("allow deploying to kube-system with instrumentWorkloads.mode=none", kubeSystemTestConfig{
				namespace:               "kube-system",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeNone,
				expectRejection:         false,
			}),
			Entry("reject deploying to kube-node-lease with instrumentWorkloads.mode=all", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
				expectRejection:         true,
			}),
			Entry("reject deploying to kube-node-lease with instrumentWorkloads.mode=created-and-updated", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				expectRejection:         true,
			}),
			Entry("allow deploying to kube-node-lease with instrumentWorkloads.mode=none", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeNone,
				expectRejection:         false,
			}),
		)

		It("should reject monitoring resources without export if no operator configuration resource exists", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
				},
			})
			Expect(err).To(MatchError(ContainSubstring("admission webhook \"validate-monitoring.dash0.com\" denied " +
				"the request: The provided Dash0 monitoring resource does not have an export configuration, and no " +
				"Dash0 operator configuration resources are available.")))
		})

		It("should reject monitoring resources without export if there is an operator configuration resource, but it is not marked as available", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
				},
			)

			// deliberately not marking the operator configuration resource as available for this test

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
				},
			})

			Expect(err).To(MatchError(ContainSubstring("admission webhook \"validate-monitoring.dash0.com\" denied " +
				"the request: The provided Dash0 monitoring resource does not have an export configuration, and no " +
				"Dash0 operator configuration resources are available.")))
		})

		It("should reject monitoring resources without export if there is an operator configuration resource, but it has no export either", func() {
			operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
				},
			)
			operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
			Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
				},
			})

			Expect(err).To(MatchError(ContainSubstring(
				"admission webhook \"validate-monitoring.dash0.com\" denied the request: The provided Dash0 " +
					"monitoring resource does not have an export configuration, and the existing Dash0 operator " +
					"configuration does not have an export configuration either.")))
		})

		It("should allow monitoring resource creation without export if there is an available operator configuration resource with an export", func() {
			operatorConfigurationResource := CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
			operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
			Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
				},
			})

			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow monitoring resource creation with export settings", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       MonitoringResourceDefaultSpec,
			})

			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject monitoring resources with an GRPC export having both insecure and insecureSkipVerify set to true", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
					Export: &dash0common.Export{
						Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           "https://example.com:1234",
							Insecure:           ptr.To(true),
							InsecureSkipVerify: ptr.To(true),
						},
					},
				},
			})

			Expect(err).To(MatchError(ContainSubstring(
				"The provided Dash0 monitoring resource has both insecure and insecureSkipVerify " +
					"explicitly enabled for the GRPC export. This is an invalid combination. " +
					"Please either set export.grpc.insecure=true or export.grpc.insecureSkipVerify=true.")))
		})

		DescribeTable("should reject monitoring resource creation with telemetry settings if the operator configuration resource has telemetry collection disabled", func(testConfig validationTestConfig) {
			operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			)
			operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
			Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       testConfig.spec,
			})

			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf(
				"admission webhook \"validate-monitoring.dash0.com\" denied the request: %s",
				testConfig.expectedError,
			))))
		},
			Entry("instrumentWorkloads.mode=all", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeAll,
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"instrumentWorkloads.mode=all. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified).",
			}),
			Entry("instrumentWorkloads.mode=created-and-updated", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"instrumentWorkloads.mode=created-and-updated. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified).",
			}),
			Entry("logCollection.enabled=true", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					LogCollection: dash0common.LogCollection{
						Enabled: ptr.To(true),
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"logCollection.enabled=true. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"logCollection.enabled=false in the monitoring resource (or leave it unspecified).",
			}),
			Entry("prometheusScraping.enabled=true", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(true),
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"prometheusScraping.enabled=true. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"prometheusScraping.enabled=false in the monitoring resource (or leave it unspecified).",
			}),
			Entry("filter!=nil", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					Filter: &dash0common.Filter{},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has filter setting. " +
					"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
					"operator configuration resource or remove the filter setting in the monitoring resource.",
			}),
			Entry("transform!=nil", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					Transform: &dash0common.Transform{},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has a transform setting " +
					"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
					"operator configuration resource or remove the transform setting in the monitoring resource.",
			}),
		)

		DescribeTable("should validate the auto-instrumentation label selector", func(testConfig validationTestConfig) {
			operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
				},
			)
			operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
			Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       testConfig.spec,
			})

			if testConfig.expectedError == "" {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf(
					"admission webhook \"validate-monitoring.dash0.com\" denied the request: %s",
					testConfig.expectedError,
				))))
			}
		},
			Entry("should allow the default value", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						// if the user omits the label selector, this default is set via
						// +kubebuilder:default=dash0.com/enable!=false
						// in api/operator/.../dash0monitoring_types.go
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedError: "",
			}),
			Entry("should reject an empty string (should not happen)", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						// This should not actually happen, due to
						// +kubebuilder:default=dash0.com/enable!=false
						// in api/operator/.../dash0monitoring_types.go
						LabelSelector: "",
					},
				},
				expectedError: "",
			}),
			Entry("should allow a custom selector with equals", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "custom/dash0-instrument=true",
					},
				},
				expectedError: "",
			}),
			Entry("should allow a custom selector with double equals", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "  custom/dash0-instrument == true  ",
					},
				},
				expectedError: "",
			}),
			Entry("should allow a custom selector with multiple equality-based requirements", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "environment=production,tier!=frontend",
					},
				},
				expectedError: "",
			}),
			Entry("should allow a complex selectors including set-based requirements", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "some-label, environment in (production, qa), tier notin (frontend, backend), stage!=dev",
					},
				},
				expectedError: "",
			}),
			Entry("should reject invalid selectors", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "environment>qa",
					},
				},
				expectedError: "The instrumentWorkloads.labelSelector setting (\"environment>qa\") in the Dash0 " +
					"monitoring resource is invalid and cannot be parsed: unable to parse requirement: values[0]: " +
					"Invalid value: \"qa\": for 'Gt', 'Lt' operators, the value must be an integer.",
			}),
		)

		DescribeTable("should validate trace context propagators", func(testConfig validationTestConfig) {
			operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
				},
			)
			operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
			Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       testConfig.spec,
			})

			if testConfig.expectedError == "" {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf(
					"admission webhook \"validate-monitoring.dash0.com\" denied the request: %s",
					testConfig.expectedError,
				))))
			}
		},
			Entry("should allow all propagators=nil", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: nil,
						},
					},
				},
				expectedError: "",
			}),
			Entry("should allow propagators = empty string", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							// setting will be ignored during workload modifications
							Propagators: ptr.To(""),
						},
					},
				},
				expectedError: "",
			}),
			Entry("should allow propagators = only whitespace", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							// setting will be ignored during workload modifications
							Propagators: ptr.To("   "),
						},
					},
				},
				expectedError: "",
			}),
			Entry("should allow single valid propagator", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
				expectedError: "",
			}),
			Entry("should allow list of valid propagators", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,baggage,b3,b3multi,jaeger,xray,ottrace,none"),
						},
					},
				},
				expectedError: "",
			}),
			Entry("should reject a single unknown propagator", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("unknown"),
						},
					},
				},
				expectedError: "The instrumentWorkloads.traceContext.propagators setting (\"unknown\") in the Dash0 " +
					"monitoring resource contains an unknown propagator value: \"unknown\". Valid trace context " +
					"propagators are tracecontext, baggage, b3, b3multi, jaeger, xray, ottrace, none. Please remove " +
					"the invalid propagator from the list.",
			}),
			Entry("should reject list with at least one unknown propagator", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,baggage,b3,unknown,b3multi,jaeger,xray,ottrace,none"),
						},
					},
				},
				expectedError: "The instrumentWorkloads.traceContext.propagators setting " +
					"(\"tracecontext,baggage,b3,unknown,b3multi,jaeger,xray,ottrace,none\") in the Dash0 monitoring " +
					"resource contains an unknown propagator value: \"unknown\". Valid trace context propagators are " +
					"tracecontext, baggage, b3, b3multi, jaeger, xray, ottrace, none. Please remove the invalid " +
					"propagator from the list.",
			}),
			Entry("should reject comma-separated list with empty items", validationTestConfig{
				spec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To(","),
						},
					},
				},
				expectedError: "The instrumentWorkloads.traceContext.propagators setting (\",\") in the Dash0 " +
					"monitoring resource contains an empty value. Please remove the empty value.",
			}),
		)

		type ottlValidationTestConfig struct {
			filter                *dash0common.Filter
			transform             *dash0common.Transform
			expectErrorSubstrings []string
		}

		DescribeTable("should validate OTTL expressions", func(testConfig ottlValidationTestConfig) {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
				},
				Spec: dash0v1beta1.Dash0MonitoringSpec{
					Export: &dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
					Filter:    testConfig.filter,
					Transform: testConfig.transform,
				},
			})

			if len(testConfig.expectErrorSubstrings) > 0 {
				for _, expectedErrorSubstring := range testConfig.expectErrorSubstrings {
					Expect(err).To(MatchError(ContainSubstring(expectedErrorSubstring)))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("should allow monitoring resource without filter or transform", ottlValidationTestConfig{}),
			Entry("should reject monitoring resource with invalid syntax in span filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Traces: &dash0common.TraceFilter{
						SpanFilter: []string{
							"invalid_syntax(...",
						},
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`unable to parse OTTL condition "invalid_syntax(...": condition has invalid syntax: 1:15: unexpected token "(" (expected <opcomparison> Value)`,
				},
			}),
			Entry("should reject monitoring resource with invalid syntax in span event filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Traces: &dash0common.TraceFilter{
						SpanEventFilter: []string{
							"invalid_syntax(...",
						},
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`unable to parse OTTL condition "invalid_syntax(...": condition has invalid syntax: 1:15: unexpected token "(" (expected <opcomparison> Value)`,
				},
			}),
			Entry("should reject monitoring resource with invalid syntax in metric filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Metrics: &dash0common.MetricFilter{
						MetricFilter: []string{
							`IsMatch(name, "^kafka\.consumer\.(?!records_consumed_total).*")`,
							`IsMatch(name, "^kafka\.producer\.(?!record_error_rate).*")`,
						},
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`unable to parse OTTL condition "IsMatch(name, \"^kafka\\.consumer\\.(?!records_consumed_total).*\")": condition has invalid syntax: 1:15: invalid quoted string "\"^kafka\\.consumer\\.(?!records_consumed_total).*\"": invalid syntax`,
					`unable to parse OTTL condition "IsMatch(name, \"^kafka\\.producer\\.(?!record_error_rate).*\")": condition has invalid syntax: 1:15: invalid quoted string "\"^kafka\\.producer\\.(?!record_error_rate).*\"": invalid syntax`,
				},
			}),
			Entry("should reject monitoring resource with invalid syntax in metric datapoint filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Metrics: &dash0common.MetricFilter{
						DataPointFilter: []string{
							"invalid_syntax(...",
						},
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`unable to parse OTTL condition "invalid_syntax(...": condition has invalid syntax: 1:15: unexpected token "(" (expected <opcomparison> Value)`,
				},
			}),
			Entry("should reject monitoring resource with invalid syntax in log record filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Logs: &dash0common.LogFilter{
						LogRecordFilter: []string{
							"invalid_syntax(...",
						},
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`unable to parse OTTL condition "invalid_syntax(...": condition has invalid syntax: 1:15: unexpected token "(" (expected <opcomparison> Value)`,
				},
			}),
			Entry("should allow monitoring resource with valid filter", ottlValidationTestConfig{
				filter: &dash0common.Filter{
					Traces: &dash0common.TraceFilter{
						SpanFilter: []string{
							`attributes["http.route"] == "/ready"`,
							`attributes["http.route"] == "/metrics"`,
						},
						SpanEventFilter: []string{
							`attributes["grpc"] == true`,
							`IsMatch(name, ".*grpc.*")`,
						},
					},
					Metrics: &dash0common.MetricFilter{
						MetricFilter: []string{
							`name == "k8s.replicaset.available"`,
							`name == "k8s.replicaset.desired"`,
						},
						DataPointFilter: []string{
							`metric.type == METRIC_DATA_TYPE_SUMMARY`,
							`resource.attributes["service.name"] == "my_service_name"`,
						},
					},
					Logs: &dash0common.LogFilter{
						LogRecordFilter: []string{
							`IsMatch(body, ".*password.*")`,
							`severity_number < SEVERITY_NUMBER_WARN`,
						},
					},
				},
			}),
			Entry("should reject monitoring resource with invalid traces transform", ottlValidationTestConfig{
				transform: &dash0common.Transform{
					Traces: []json.RawMessage{
						[]byte(`"invalid_syntax(..."`),
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`statement has invalid syntax: 1:16: unexpected token "." (expected ")" Key*)`,
				},
			}),
			Entry("should reject monitoring resource with invalid metrics transform", ottlValidationTestConfig{
				transform: &dash0common.Transform{
					Metrics: []json.RawMessage{
						[]byte(`"invalid_syntax(..."`),
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`statement has invalid syntax: 1:16: unexpected token "." (expected ")" Key*)`,
				},
			}),
			Entry("should reject monitoring resource with invalid logs transform", ottlValidationTestConfig{
				transform: &dash0common.Transform{
					Logs: []json.RawMessage{
						[]byte(`"invalid_syntax(..."`),
					},
				},
				expectErrorSubstrings: []string{
					`admission webhook "validate-monitoring.dash0.com" denied the request: `,
					`statement has invalid syntax: 1:16: unexpected token "." (expected ")" Key*)`,
				},
			}),
			Entry("should allow monitoring resource with valid transform", ottlValidationTestConfig{
				transform: &dash0common.Transform{
					Traces: []json.RawMessage{
						[]byte(`"truncate_all(span.attributes, 1024)"`),
					},
					Metrics: []json.RawMessage{
						[]byte(`{
					        "conditions": [ "metric.type == METRIC_DATA_TYPE_SUM" ],
					        "statements": [ "truncate_all(datapoint.attributes, 1024)" ]
					    }`),
					},
					Logs: []json.RawMessage{
						[]byte(`"truncate_all(log.attributes, 1024)"`),
					},
				},
			}),
		)
	})
})
