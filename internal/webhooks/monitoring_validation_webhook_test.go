// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type kubeSystemTestConfig struct {
	namespace               string
	instrumentWorkloadsMode dash0v1alpha1.InstrumentWorkloadsMode
	expectRejection         bool
}

type monitoringResourceValidationWithTelemetryCollectionOffTestConfig struct {
	spec          dash0v1alpha1.Dash0MonitoringSpec
	expectedError string
}

type ottlValidationTestConfig struct {
	filter                *dash0v1alpha1.Filter
	transform             *dash0v1alpha1.Transform
	expectErrorSubstrings []string
}

var _ = Describe("The validation webhook for the monitoring resource", func() {

	AfterEach(func() {
		Expect(
			k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0Monitoring{}, client.InNamespace(TestNamespaceName)),
		).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
	})

	Describe("when validating", Ordered, func() {

		DescribeTable("should reject deploying a monitoring resources to kube-system unless instrumentWorkloads.mode=none", func(testConfig kubeSystemTestConfig) {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testConfig.namespace,
					Name:      MonitoringResourceName,
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: testConfig.instrumentWorkloadsMode,
					},
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0v1alpha1.Authorization{
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
				instrumentWorkloadsMode: dash0v1alpha1.All,
				expectRejection:         true,
			}),
			Entry("reject deploying to kube-system with instrumentWorkloads.mode=created-and-updated", kubeSystemTestConfig{
				namespace:               "kube-system",
				instrumentWorkloadsMode: dash0v1alpha1.CreatedAndUpdated,
				expectRejection:         true,
			}),
			Entry("allow deploying to kube-system with instrumentWorkloads.mode=none", kubeSystemTestConfig{
				namespace:               "kube-system",
				instrumentWorkloadsMode: dash0v1alpha1.None,
				expectRejection:         false,
			}),
			Entry("reject deploying to kube-node-lease with instrumentWorkloads.mode=all", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0v1alpha1.All,
				expectRejection:         true,
			}),
			Entry("reject deploying to kube-node-lease with instrumentWorkloads.mode=created-and-updated", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0v1alpha1.CreatedAndUpdated,
				expectRejection:         true,
			}),
			Entry("allow deploying to kube-node-lease with instrumentWorkloads.mode=none", kubeSystemTestConfig{
				namespace:               "kube-node-lease",
				instrumentWorkloadsMode: dash0v1alpha1.None,
				expectRejection:         false,
			}),
		)

		It("should reject monitoring resources without export if no operator configuration resource exists", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.All,
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

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.All,
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

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.All,
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

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.All,
					},
				},
			})

			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow monitoring resource creation with export settings", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       MonitoringResourceDefaultSpec,
			})

			Expect(err).ToNot(HaveOccurred())
		})

		DescribeTable("should reject monitoring resource creation with telemetry settings if the operator configuration resource has telemetry collection disabled", func(testConfig monitoringResourceValidationWithTelemetryCollectionOffTestConfig) {
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

			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec:       testConfig.spec,
			})

			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf(
				"admission webhook \"validate-monitoring.dash0.com\" denied the request: %s",
				testConfig.expectedError,
			))))
		},
			Entry("instrumentWorkloads.mode=all", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.All,
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"instrumentWorkloads.mode=all. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified).",
			}),
			Entry("instrumentWorkloads.mode=created-and-updated", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1alpha1.InstrumentWorkloads{
						Mode: dash0v1alpha1.CreatedAndUpdated,
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"instrumentWorkloads.mode=created-and-updated. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"instrumentWorkloads.mode=none in the monitoring resource (or leave it unspecified).",
			}),
			Entry("logCollection.enabled=true", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					LogCollection: dash0v1alpha1.LogCollection{
						Enabled: ptr.To(true),
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"logCollection.enabled=true. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"logCollection.enabled=false in the monitoring resource (or leave it unspecified).",
			}),
			Entry("prometheusScraping.enabled=true", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScraping: dash0v1alpha1.PrometheusScraping{
						Enabled: ptr.To(true),
					},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"prometheusScraping.enabled=true. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"prometheusScraping.enabled=false in the monitoring resource (or leave it unspecified).",
			}),
			Entry("prometheusScrapingEnabled=true", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScrapingEnabled: ptr.To(true),
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has the setting " +
					"prometheusScrapingEnabled=true. This is an invalid combination. Please either set " +
					"telemetryCollection.enabled=true in the operator configuration resource or set " +
					"prometheusScrapingEnabled=false in the monitoring resource (or leave it unspecified).",
			}),
			Entry("filter!=nil", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					Filter: &dash0v1alpha1.Filter{},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has filter setting. " +
					"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
					"operator configuration resource or remove the filter setting in the monitoring resource.",
			}),
			Entry("transform!=nil", monitoringResourceValidationWithTelemetryCollectionOffTestConfig{
				spec: dash0v1alpha1.Dash0MonitoringSpec{
					Transform: &dash0v1alpha1.Transform{},
				},
				expectedError: "The Dash0 operator configuration resource has telemetry collection disabled " +
					"(telemetryCollection.enabled=false), and yet the monitoring resource has a transform setting " +
					"This is an invalid combination. Please either set telemetryCollection.enabled=true in the " +
					"operator configuration resource or remove the transform setting in the monitoring resource.",
			}),
		)

		DescribeTable("should validate OTTL expressions", func(testConfig ottlValidationTestConfig) {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0v1alpha1.Authorization{
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
				filter: &dash0v1alpha1.Filter{
					Traces: &dash0v1alpha1.TraceFilter{
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
				filter: &dash0v1alpha1.Filter{
					Traces: &dash0v1alpha1.TraceFilter{
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
				filter: &dash0v1alpha1.Filter{
					Metrics: &dash0v1alpha1.MetricFilter{
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
				filter: &dash0v1alpha1.Filter{
					Metrics: &dash0v1alpha1.MetricFilter{
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
				filter: &dash0v1alpha1.Filter{
					Logs: &dash0v1alpha1.LogFilter{
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
				filter: &dash0v1alpha1.Filter{
					Traces: &dash0v1alpha1.TraceFilter{
						SpanFilter: []string{
							`attributes["http.route"] == "/ready"`,
							`attributes["http.route"] == "/metrics"`,
						},
						SpanEventFilter: []string{
							`attributes["grpc"] == true`,
							`IsMatch(name, ".*grpc.*")`,
						},
					},
					Metrics: &dash0v1alpha1.MetricFilter{
						MetricFilter: []string{
							`name == "k8s.replicaset.available"`,
							`name == "k8s.replicaset.desired"`,
						},
						DataPointFilter: []string{
							`metric.type == METRIC_DATA_TYPE_SUMMARY`,
							`resource.attributes["service.name"] == "my_service_name"`,
						},
					},
					Logs: &dash0v1alpha1.LogFilter{
						LogRecordFilter: []string{
							`IsMatch(body, ".*password.*")`,
							`severity_number < SEVERITY_NUMBER_WARN`,
						},
					},
				},
			}),
			Entry("should reject monitoring resource with invalid traces transform", ottlValidationTestConfig{
				transform: &dash0v1alpha1.Transform{
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
				transform: &dash0v1alpha1.Transform{
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
				transform: &dash0v1alpha1.Transform{
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
				transform: &dash0v1alpha1.Transform{
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
