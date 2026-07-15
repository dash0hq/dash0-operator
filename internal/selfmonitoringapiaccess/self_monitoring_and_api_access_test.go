// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	exporters "github.com/dash0hq/dash0-operator/internal/util/exporters"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe(
	"self monitoring and API access", Ordered, func() {

		ctx := context.Background()
		logger := logd.FromContext(ctx)

		Describe(
			"convert operator configuration resource to self monitoring settings", func() {

				type resourceToSelfMonitoringTestConfig struct {
					operatorConfigurationSpec           *dash0v1alpha1.Dash0OperatorConfigurationSpec
					secret                              *corev1.Secret
					expectError                         bool
					expectedSelfMonitoringConfiguration SelfMonitoringConfiguration
				}

				DescribeTable(
					"should convert the operator configuration resource to self monitoring configuration",
					func(testConfig resourceToSelfMonitoringTestConfig) {
						if testConfig.secret != nil {
							EnsureOperatorNamespaceExists(ctx, k8sClient)
							Expect(k8sClient.Create(ctx, testConfig.secret)).To(Succeed())
							defer func() {
								Expect(k8sClient.Delete(ctx, testConfig.secret)).To(Succeed())
							}()
						}
						var operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration
						if testConfig.operatorConfigurationSpec != nil {
							operatorConfigurationResource = &dash0v1alpha1.Dash0OperatorConfiguration{
								ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
								Spec:       *testConfig.operatorConfigurationSpec,
							}
						}
						selfMonitoringConfiguration, err := ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
							ctx,
							k8sClient,
							OperatorNamespace,
							operatorConfigurationResource,
							logger,
						)
						if testConfig.expectError {
							Expect(err).To(HaveOccurred())
							return
						}

						Expect(err).ToNot(HaveOccurred())
						Expect(selfMonitoringConfiguration).To(Equal(testConfig.expectedSelfMonitoringConfiguration))
					},
					Entry(
						"self monitoring is not activated if there is no operator configuration resource",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec:           nil,
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{SelfMonitoringEnabled: false},
						},
					),
					Entry(
						"self monitoring is not activated if it isn't enabled",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: false,
							},
						},
					),
					Entry(
						"self monitoring is not activated if there is no export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        nil,
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: false,
							},
						},
					),
					Entry(
						"self monitoring is not activated if there is an export struct with no export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{},
								},
							},
							expectError: true,
						},
					),
					Entry(
						"should convert Dash0 export with token",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        Dash0ExportWithEndpointAndToken().ToExports(),
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *Dash0ExportWithEndpointAndToken(),
								Token:                 &AuthorizationTokenTest,
							},
						},
					),
					Entry(
						"should convert Dash0 export with secret ref",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        Dash0ExportWithEndpointAndSecretRef().ToExports(),
							},
							secret: DefaultSecret(),
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *Dash0ExportWithEndpointAndSecretRef(),
								Token:                 &AuthorizationTokenTestFromSecret,
							},
						},
					),
					Entry(
						"should use custom dataset",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        Dash0ExportWithEndpointTokenAndCustomDataset().ToExports(),
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *Dash0ExportWithEndpointTokenAndCustomDataset(),
								Token:                 &AuthorizationTokenTest,
							},
						},
					),
					Entry(
						"should ignore grpc and http exports if a Dash0 export is present",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0Test,
											Authorization: dash0common.Authorization{
												Token: &AuthorizationTokenTest,
											},
										},
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint: EndpointGrpcTest,
										},
										Http: &dash0common.HttpConfiguration{
											Endpoint: EndpointHttpTest,
											Encoding: dash0common.Proto,
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *Dash0ExportWithEndpointAndToken(),
								Token:                 &AuthorizationTokenTest,
							},
						},
					),
					Entry(
						"should convert gRPC export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        GrpcExportTest().ToExports(),
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *GrpcExportTest(),
							},
						},
					),
					Entry(
						"should convert HTTP export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports:        HttpExportTest().ToExports(),
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *HttpExportTest(),
							},
						},
					),
					Entry(
						"should resolve a secret-backed gRPC header value to its literal value",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint: EndpointGrpcTest,
											Headers: []dash0common.Header{
												{
													Name: "Authorization",
													ValueFrom: &dash0common.HeaderValueFrom{
														SecretKeyRef: &dash0common.SecretKeySelector{
															Name: SecretRefTest.Name,
															Key:  SecretRefTest.Key,
														},
													},
												},
											},
										},
									},
								},
							},
							secret: DefaultSecret(),
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export: dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Headers: []dash0common.Header{
											{
												Name: "Authorization",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: SecretRefTest.Name,
														Key:  SecretRefTest.Key,
													},
												},
											},
										},
									},
								},
								ResolvedSecretHeaderValues: map[string]string{
									"Authorization": AuthorizationTokenTestFromSecret,
								},
							},
						},
					),
					Entry(
						"should resolve a secret-backed HTTP header value to its literal value",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Http: &dash0common.HttpConfiguration{
											Endpoint: EndpointHttpTest,
											Encoding: dash0common.Proto,
											Headers: []dash0common.Header{
												{
													Name: "X-Api-Key",
													ValueFrom: &dash0common.HeaderValueFrom{
														SecretKeyRef: &dash0common.SecretKeySelector{
															Name: SecretRefTest.Name,
															Key:  SecretRefTest.Key,
														},
													},
												},
											},
										},
									},
								},
							},
							secret: DefaultSecret(),
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export: dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Encoding: dash0common.Proto,
										Headers: []dash0common.Header{
											{
												Name: "X-Api-Key",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: SecretRefTest.Name,
														Key:  SecretRefTest.Key,
													},
												},
											},
										},
									},
								},
								ResolvedSecretHeaderValues: map[string]string{
									"X-Api-Key": AuthorizationTokenTestFromSecret,
								},
							},
						},
					),
					Entry(
						"should disable self-monitoring if a secret-backed header value cannot be resolved",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint: EndpointGrpcTest,
											Headers: []dash0common.Header{
												{
													Name: "Authorization",
													ValueFrom: &dash0common.HeaderValueFrom{
														SecretKeyRef: &dash0common.SecretKeySelector{
															Name: "does-not-exist",
															Key:  "key",
														},
													},
												},
											},
										},
									},
								},
							},
							// no secret is created, so the secret ref cannot be resolved
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: false,
							},
						},
					),
					Entry(
						"multiple exports: should use the first Dash0 export for self-monitoring",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0Test,
											Authorization: dash0common.Authorization{
												Token: &AuthorizationTokenTest,
											},
										},
									},
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0TestAlternative,
											Authorization: dash0common.Authorization{
												Token: &AuthorizationTokenTestAlternative,
											},
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *Dash0ExportWithEndpointAndToken(),
								Token:                 &AuthorizationTokenTest,
							},
						},
					),
					Entry(
						"multiple exports: should use the first gRPC export if it comes before a Dash0 export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint: EndpointGrpcTest,
											Headers: []dash0common.Header{
												{
													Name:  "Key",
													Value: "Value",
												},
											},
										},
									},
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0Test,
											Authorization: dash0common.Authorization{
												Token: &AuthorizationTokenTest,
											},
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export:                *GrpcExportTest(),
							},
						},
					),
					Entry(
						"multiple exports: should disable self-monitoring if the first export token cannot be resolved",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0Test,
											Authorization: dash0common.Authorization{
												SecretRef: &SecretRefTest, // secret does not exist
											},
										},
									},
									{
										Dash0: &dash0common.Dash0Configuration{
											Endpoint: EndpointDash0TestAlternative,
											Authorization: dash0common.Authorization{
												Token: &AuthorizationTokenTestAlternative,
											},
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: false,
							},
						},
					),
				)
			},
		)

		Describe(
			"convert export settings to env vars", func() {

				type exportToEnvVarsTestConfig struct {
					export                     dash0common.Export
					expectedEndpointAndHeaders EndpointAndHeaders
				}

				DescribeTable(
					"should convert the export self monitoring env vars", func(testConfig exportToEnvVarsTestConfig) {
						endpointAndHeaders := ConvertExportConfigurationToEnvVarSettings(testConfig.export)
						Expect(endpointAndHeaders).To(Equal(testConfig.expectedEndpointAndHeaders))
					},
					Entry(
						"should return empty result if there are no exports",
						exportToEnvVarsTestConfig{
							export:                     dash0common.Export{},
							expectedEndpointAndHeaders: EndpointAndHeaders{},
						},
					),
					Entry(
						"should convert Dash0 export with token",
						exportToEnvVarsTestConfig{
							export: *Dash0ExportWithEndpointAndToken(),
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointDash0WithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  util.AuthorizationHeaderName,
										Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
									},
								},
							},
						},
					),
					Entry(
						"should convert Dash0 export with secret ref",
						exportToEnvVarsTestConfig{
							export: *Dash0ExportWithEndpointAndSecretRef(),
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointDash0WithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  util.AuthorizationHeaderName,
										Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
									},
								},
							},
						},
					),
					Entry(
						"should use custom dataset",
						exportToEnvVarsTestConfig{
							export: *Dash0ExportWithEndpointTokenAndCustomDataset(),
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointDash0WithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  util.AuthorizationHeaderName,
										Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
									},
									{
										Name:  util.Dash0DatasetHeaderName,
										Value: DatasetCustomTest,
									},
								},
							},
						},
					),
					Entry(
						"should ignore grpc and http exports if a Dash0 export is present",
						exportToEnvVarsTestConfig{
							export: dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0Test,
									Authorization: dash0common.Authorization{
										Token: &AuthorizationTokenTest,
									},
								},
								Grpc: &dash0common.GrpcConfiguration{
									Endpoint: EndpointGrpcTest,
								},
								Http: &dash0common.HttpConfiguration{
									Endpoint: EndpointHttpTest,
									Encoding: dash0common.Proto,
								},
							},
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointDash0WithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  util.AuthorizationHeaderName,
										Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
									},
								},
							},
						},
					),
					Entry(
						"should convert gRPC export",
						exportToEnvVarsTestConfig{
							export: dash0common.Export{
								Grpc: &dash0common.GrpcConfiguration{
									Endpoint: EndpointGrpcWithProtocolTest,
									Headers: []dash0common.Header{
										{
											Name:  "Key",
											Value: "Value",
										},
									},
								},
							},
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointGrpcWithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  "Key",
										Value: "Value",
									},
								},
							},
						},
					),
					Entry(
						"should convert gRPC export and prepend protocol",
						exportToEnvVarsTestConfig{
							export: *GrpcExportTest(),
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointGrpcWithProtocolTest,
								Protocol: common.ProtocolGrpc,
								Headers: []dash0common.Header{
									{
										Name:  "Key",
										Value: "Value",
									},
								},
							},
						},
					),
					Entry(
						"should convert HTTP/protobuf export",
						exportToEnvVarsTestConfig{
							export: *HttpExportTest(),
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointHttpTest,
								Protocol: common.ProtocolHttpProtobuf,
								Headers: []dash0common.Header{
									{
										Name:  "Key",
										Value: "Value",
									},
								},
							},
						},
					),
					Entry(
						"should overide HTTP/json to HTTP/protobuf",
						exportToEnvVarsTestConfig{
							export: dash0common.Export{
								Http: &dash0common.HttpConfiguration{
									Endpoint: EndpointHttpTest,
									Headers: []dash0common.Header{
										{
											Name:  "Key",
											Value: "Value",
										},
									},
									Encoding: dash0common.Json,
								},
							},
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: EndpointHttpTest,
								Protocol: common.ProtocolHttpProtobuf,
								Headers: []dash0common.Header{
									{
										Name:  "Key",
										Value: "Value",
									},
								},
							},
						},
					),
				)

				type prependProtocolTestConfig struct {
					endpoint        string
					defaultProtocol string
					wanted          string
				}

				DescribeTable(
					"should convert the export self monitoring env vars", func(testConfig prependProtocolTestConfig) {
						result := prependProtocol(testConfig.endpoint, testConfig.defaultProtocol)
						Expect(result).To(Equal(testConfig.wanted))
					},
					Entry(
						"should prepend protocol if there is none",
						prependProtocolTestConfig{
							endpoint:        "endpoint.backend.com:4317",
							defaultProtocol: "https://",
							wanted:          "https://endpoint.backend.com:4317",
						},
					),
					Entry(
						"should not prepend protocol if endpoint has http protocol",
						prependProtocolTestConfig{
							endpoint:        "http://endpoint.backend.com:4317",
							defaultProtocol: "https://",
							wanted:          "http://endpoint.backend.com:4317",
						},
					),
					Entry(
						"should not prepend protocol if endpoint has https protocol",
						prependProtocolTestConfig{
							endpoint:        "HTTPS://endpoint.backend.com:4317",
							defaultProtocol: "https://",
							wanted:          "HTTPS://endpoint.backend.com:4317",
						},
					),
					Entry(
						"should not prepend protocol if endpoint has dns protocol",
						prependProtocolTestConfig{
							endpoint:        "dns://endpoint.backend.com:4317",
							defaultProtocol: "https://",
							wanted:          "dns://endpoint.backend.com:4317",
						},
					),
				)
			},
		)

		Describe(
			"enable self-monitoring in collector containers with secret-backed headers", func() {

				const (
					secretName = "dash0-authorization-secret"
					secretKey  = "token"
				)

				// verifySecretBackedHeaderWiring asserts that a secret-backed self-monitoring header ("Authorization")
				// is injected as a dedicated environment variable (backed by the secret) and referenced from
				// OTEL_EXPORTER_OTLP_HEADERS via $(...), while a literal header ("Dash0-Dataset") is rendered inline. The
				// referenced env var must precede OTEL_EXPORTER_OTLP_HEADERS so Kubernetes can expand the reference.
				verifySecretBackedHeaderWiring := func(container corev1.Container, protocol string) {
					headersIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)
					Expect(headersIdx).To(
						BeNumerically(">=", 0),
						fmt.Sprintf("container %s should have an OTEL_EXPORTER_OTLP_HEADERS env var", container.Name),
					)

					// The secret-backed header is the second header (index 1), after the literal Dash0-Dataset header.
					expectedSecretEnvVarName := exporters.HeaderSecretEnvVarName(protocol, "self_monitoring", 1)
					Expect(container.Env[headersIdx].Value).To(Equal(
						fmt.Sprintf(
							"Dash0-Dataset=default,%s=$(%s)",
							util.AuthorizationHeaderName,
							expectedSecretEnvVarName,
						),
					))

					secretIdx := slices.IndexFunc(container.Env, matchEnvVar(expectedSecretEnvVarName))
					Expect(secretIdx).To(
						BeNumerically(">=", 0),
						fmt.Sprintf("container %s should have the secret-backed header env var", container.Name),
					)
					Expect(secretIdx).To(
						BeNumerically("<", headersIdx),
						"the secret-backed header env var must be defined before OTEL_EXPORTER_OTLP_HEADERS",
					)

					secretEnvVar := container.Env[secretIdx]
					Expect(secretEnvVar.Value).To(BeEmpty())
					Expect(secretEnvVar.ValueFrom).NotTo(BeNil())
					Expect(secretEnvVar.ValueFrom.SecretKeyRef).NotTo(BeNil())
					Expect(secretEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal(secretName))
					Expect(secretEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal(secretKey))
				}

				secretHeaders := func() []dash0common.Header {
					return []dash0common.Header{
						{Name: util.Dash0DatasetHeaderName, Value: "default"},
						{
							Name: util.AuthorizationHeaderName,
							ValueFrom: &dash0common.HeaderValueFrom{
								SecretKeyRef: &dash0common.SecretKeySelector{Name: secretName, Key: secretKey},
							},
						},
					}
				}

				// Mimics the collector containers, including the configuration-reloader which does not carry the
				// data-pipeline header env vars, so the self-monitoring path must inject its own.
				collectorContainers := func() []corev1.Container {
					return []corev1.Container{
						{Name: "opentelemetry-collector"},
						{Name: "configuration-reloader"},
					}
				}

				It("should wire secret-backed headers for an HTTP export in every collector container", func() {
					containers := collectorContainers()
					err := enableSelfMonitoringInCollector(
						containers,
						SelfMonitoringConfiguration{
							SelfMonitoringEnabled: true,
							Export: dash0common.Export{
								Http: &dash0common.HttpConfiguration{
									Endpoint: EndpointHttpTest,
									Encoding: dash0common.Proto,
									Headers:  secretHeaders(),
								},
							},
						},
						"1.2.3",
						false,
					)
					Expect(err).NotTo(HaveOccurred())
					for _, container := range containers {
						verifySecretBackedHeaderWiring(container, "HTTP")
					}
				})

				It("should wire secret-backed headers for a gRPC export in every collector container", func() {
					containers := collectorContainers()
					err := enableSelfMonitoringInCollector(
						containers,
						SelfMonitoringConfiguration{
							SelfMonitoringEnabled: true,
							Export: dash0common.Export{
								Grpc: &dash0common.GrpcConfiguration{
									Endpoint: EndpointGrpcTest,
									Headers:  secretHeaders(),
								},
							},
						},
						"1.2.3",
						false,
					)
					Expect(err).NotTo(HaveOccurred())
					for _, container := range containers {
						verifySecretBackedHeaderWiring(container, "GRPC")
					}
				})
			},
		)

		Describe(
			"convert export settings to collector metrics self-monitoring pipeline string", func() {

				type exportToCollectorMetricsSelfMonitoringPipelineTestConfig struct {
					selfMonitoringConfiguration   SelfMonitoringConfiguration
					expectedMetricsPipelineString string
				}

				var (
					dash0ExportExpectedMetricsPipelineString = expectedMetricsPipeline(`
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`)

					dash0ExportWithCustomDatasetExpectedMetricsPipelineString = expectedMetricsPipeline(`
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
                  - name: Dash0-Dataset
                    value: "test-dataset"
`)
				)

				DescribeTable(
					"should convert the self monitoring configuration",
					func(testConfig exportToCollectorMetricsSelfMonitoringPipelineTestConfig) {
						logPipeline := ConvertExportConfigurationToCollectorMetricsSelfMonitoringPipelineString(testConfig.selfMonitoringConfiguration)
						Expect(logPipeline).To(Equal(testConfig.expectedMetricsPipelineString))
					},
					Entry(
						"should return empty result if self monitoring is disabled",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration:   SelfMonitoringConfiguration{SelfMonitoringEnabled: false},
							expectedMetricsPipelineString: "",
						},
					),
					Entry(
						"should return empty result if there are no exports",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration:   createSelfMonitoringConfiguration(&dash0common.Export{}),
							expectedMetricsPipelineString: "",
						},
					),
					Entry(
						"should convert Dash0 export with token",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration:   createSelfMonitoringConfiguration(Dash0ExportWithEndpointAndToken()),
							expectedMetricsPipelineString: dash0ExportExpectedMetricsPipelineString,
						},
					),
					Entry(
						"should convert Dash0 export with secret ref",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration:   createSelfMonitoringConfiguration(Dash0ExportWithEndpointAndSecretRef()),
							expectedMetricsPipelineString: dash0ExportExpectedMetricsPipelineString,
						},
					),
					Entry(
						"should use custom dataset",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration:   createSelfMonitoringConfiguration(Dash0ExportWithEndpointTokenAndCustomDataset()),
							expectedMetricsPipelineString: dash0ExportWithCustomDatasetExpectedMetricsPipelineString,
						},
					),
					Entry(
						"should ignore grpc and http exports if a Dash0 export is present",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Dash0: &dash0common.Dash0Configuration{
										Endpoint: EndpointDash0Test,
										Authorization: dash0common.Authorization{
											Token: &AuthorizationTokenTest,
										},
									},
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
									},
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedMetricsPipelineString: dash0ExportExpectedMetricsPipelineString,
						},
					),
					Entry(
						"should convert non-TLS Dash0 export",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Dash0: &dash0common.Dash0Configuration{
										Endpoint: "http://endpoint.dash0.com:4317",
										Authorization: dash0common.Authorization{
											Token: &AuthorizationTokenTest,
										},
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: grpc
                endpoint: http://endpoint.dash0.com:4317
                insecure: true
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`),
						},
					),
					Entry(
						"should convert gRPC export without headers",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
`),
						},
					),
					Entry(
						"should convert gRPC export",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`),
						},
					),
					Entry(
						"should convert non-TLS gRPC export",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: "http://endpoint.backend.com:4317",
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: grpc
                endpoint: http://endpoint.backend.com:4317
                insecure: true
`),
						},
					),
					Entry(
						"should render a secret-backed gRPC header as an env var reference",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Headers: []dash0common.Header{
											{
												Name:  "X-Plain",
												Value: "plain-value",
											},
											{
												Name: "Authorization",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: "my-secret",
														Key:  "api-key",
													},
												},
											},
										},
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  - name: X-Plain
                    value: "plain-value"
                  - name: Authorization
                    value: "${env:DASH0_HEADER_GRPC_DEFAULT_0_1}"
`),
						},
					),
					Entry(
						"should convert HTTP/protobuf export",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`),
						},
					),
					Entry(
						"should convert HTTP/json export",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
										Encoding: dash0common.Json,
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: http/json
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`),
						},
					),
					Entry(
						"should render a secret-backed HTTP header as an env var reference",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name: "X-Api-Key",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: "my-secret",
														Key:  "api-key",
													},
												},
											},
										},
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedMetricsPipelineString: expectedMetricsPipeline(`
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: X-Api-Key
                    value: "${env:DASH0_HEADER_HTTP_DEFAULT_0_0}"
`),
						},
					),
				)
			},
		)

		Describe(
			"convert export settings to collector logs self-monitoring pipeline string", func() {

				type exportToCollectorLogsSelfMonitoringPipelineTestConfig struct {
					selfMonitoringConfiguration SelfMonitoringConfiguration
					expectedLogsPipelineString  string
				}

				var (
					dash0ExportExpectedLogsPipelineString = `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`

					dash0ExportWithCustomDatasetExpectedLogPipelineString = `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
                  - name: Dash0-Dataset
                    value: "test-dataset"
`
				)

				DescribeTable(
					"should convert the self monitoring configuration",
					func(testConfig exportToCollectorLogsSelfMonitoringPipelineTestConfig) {
						logPipeline := ConvertExportConfigurationToCollectorLogsSelfMonitoringPipelineString(testConfig.selfMonitoringConfiguration)
						Expect(logPipeline).To(Equal(testConfig.expectedLogsPipelineString))
					},
					Entry(
						"should return empty result if self monitoring is disabled",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: SelfMonitoringConfiguration{SelfMonitoringEnabled: false},
							expectedLogsPipelineString:  "",
						},
					),
					Entry(
						"should return empty result if there are no exports",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(&dash0common.Export{}),
							expectedLogsPipelineString:  "",
						},
					),
					Entry(
						"should convert Dash0 export with token",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(Dash0ExportWithEndpointAndToken()),
							expectedLogsPipelineString:  dash0ExportExpectedLogsPipelineString,
						},
					),
					Entry(
						"should convert Dash0 export with secret ref",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(Dash0ExportWithEndpointAndSecretRef()),
							expectedLogsPipelineString:  dash0ExportExpectedLogsPipelineString,
						},
					),
					Entry(
						"should use custom dataset",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(Dash0ExportWithEndpointTokenAndCustomDataset()),
							expectedLogsPipelineString:  dash0ExportWithCustomDatasetExpectedLogPipelineString,
						},
					),
					Entry(
						"should ignore grpc and http exports if a Dash0 export is present",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Dash0: &dash0common.Dash0Configuration{
										Endpoint: EndpointDash0Test,
										Authorization: dash0common.Authorization{
											Token: &AuthorizationTokenTest,
										},
									},
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
									},
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedLogsPipelineString: dash0ExportExpectedLogsPipelineString,
						},
					),
					Entry(
						"should convert non-TLS Dash0 export",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Dash0: &dash0common.Dash0Configuration{
										Endpoint: "http://endpoint.dash0.com:4317",
										Authorization: dash0common.Authorization{
											Token: &AuthorizationTokenTest,
										},
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://endpoint.dash0.com:4317
                insecure: true
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`,
						},
					),
					Entry(
						"should convert gRPC export without headers",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
`,
						},
					),
					Entry(
						"should convert gRPC export",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`,
						},
					),
					Entry(
						"should convert non-TLS gRPC export",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: "http://endpoint.backend.com:4317",
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://endpoint.backend.com:4317
                insecure: true
`,
						},
					),
					Entry(
						"should render a secret-backed gRPC header as an env var reference",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Headers: []dash0common.Header{
											{
												Name:  "X-Plain",
												Value: "plain-value",
											},
											{
												Name: "Authorization",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: "my-secret",
														Key:  "api-key",
													},
												},
											},
										},
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  - name: X-Plain
                    value: "plain-value"
                  - name: Authorization
                    value: "${env:DASH0_HEADER_GRPC_DEFAULT_0_1}"
`,
						},
					),
					Entry(
						"should convert HTTP/protobuf export",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`,
						},
					),
					Entry(
						"should convert HTTP/json export",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name:  "Key1",
												Value: "Value1",
											},
											{
												Name:  "Key2",
												Value: "Value2",
											},
											{
												Name: "KeyWithoutValue",
											},
											{
												Value: "ValueWithoutName",
											},
										},
										Encoding: dash0common.Json,
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/json
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: Key1
                    value: "Value1"
                  - name: Key2
                    value: "Value2"
                  - name: KeyWithoutValue
                    value: ""
`,
						},
					),
					Entry(
						"should render a secret-backed HTTP header as an env var reference",
						exportToCollectorLogsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint: EndpointHttpTest,
										Headers: []dash0common.Header{
											{
												Name: "X-Api-Key",
												ValueFrom: &dash0common.HeaderValueFrom{
													SecretKeyRef: &dash0common.SecretKeySelector{
														Name: "my-secret",
														Key:  "api-key",
													},
												},
											},
										},
										Encoding: dash0common.Proto,
									},
								},
							),
							expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  - name: X-Api-Key
                    value: "${env:DASH0_HEADER_HTTP_DEFAULT_0_0}"
`,
						},
					),
				)
			},
		)

		Describe(
			"GetAuthTokenForDash0Export", func() {
				type getAuthTokenTestConfig struct {
					dash0Export   dash0common.Dash0Configuration
					secret        *corev1.Secret
					expectedError string
					expectedToken string
				}

				var createdObjectsGetAuthToken []client.Object

				AfterEach(
					func() {
						createdObjectsGetAuthToken = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsGetAuthToken)
					},
				)

				DescribeTable(
					"should resolve the auth token", func(testConfig getAuthTokenTestConfig) {
						if testConfig.secret != nil {
							EnsureOperatorNamespaceExists(ctx, k8sClient)
							Expect(k8sClient.Create(ctx, testConfig.secret)).To(Succeed())
							createdObjectsGetAuthToken = append(createdObjectsGetAuthToken, testConfig.secret)
						}
						token, err := GetAuthTokenForDash0Export(
							ctx,
							k8sClient,
							OperatorNamespace,
							testConfig.dash0Export,
							logger,
						)
						if testConfig.expectedError != "" {
							Expect(err).To(HaveOccurred())
							Expect(err.Error()).To(ContainSubstring(testConfig.expectedError))
						} else {
							Expect(err).ToNot(HaveOccurred())
							Expect(token).NotTo(BeNil())
							Expect(*token).To(Equal(testConfig.expectedToken))
						}
					},
					Entry(
						"should return the token literal if present", getAuthTokenTestConfig{
							dash0Export: dash0common.Dash0Configuration{
								Endpoint: EndpointDash0Test,
								Authorization: dash0common.Authorization{
									Token: &AuthorizationTokenTest,
								},
							},
							expectedToken: AuthorizationTokenTest,
						},
					),
					Entry(
						"should resolve secret ref and return the token", getAuthTokenTestConfig{
							dash0Export: dash0common.Dash0Configuration{
								Endpoint:    EndpointDash0Test,
								ApiEndpoint: ApiEndpointTest,
								Authorization: dash0common.Authorization{
									SecretRef: &SecretRefTest,
								},
							},
							secret:        DefaultSecret(),
							expectedToken: AuthorizationTokenTestFromSecret,
						},
					),
					Entry(
						"should error if there is neither a token nor a secret ref", getAuthTokenTestConfig{
							dash0Export: dash0common.Dash0Configuration{
								Endpoint: EndpointDash0Test,
							},
							expectedError: "authorization has neither secretRef nor token literal",
						},
					),
					Entry(
						"should error if the secret does not exist", getAuthTokenTestConfig{
							dash0Export: dash0common.Dash0Configuration{
								Endpoint:    EndpointDash0Test,
								ApiEndpoint: ApiEndpointTest,
								Authorization: dash0common.Authorization{
									SecretRef: &SecretRefTest,
								},
							},
							expectedError: "failed to fetch secret with name secret-ref in namespace test-operator-namespace",
						},
					),
				)
			},
		)

		Describe(
			"ExchangeSecretRefForToken", func() {
				type exchangeTestConfig struct {
					dash0Config   dash0common.Dash0Configuration
					secret        *corev1.Secret
					expectedError string
					expectedToken string
				}

				var createdObjectsExchange []client.Object

				AfterEach(
					func() {
						createdObjectsExchange = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsExchange)
					},
				)

				DescribeTable(
					"should fetch and decode the auth token", func(testConfig exchangeTestConfig) {
						if testConfig.secret != nil {
							EnsureOperatorNamespaceExists(ctx, k8sClient)
							Expect(k8sClient.Create(ctx, testConfig.secret)).To(Succeed())
							createdObjectsExchange = append(createdObjectsExchange, testConfig.secret)
						}
						token, err := ExchangeSecretRefForToken(
							ctx,
							k8sClient,
							OperatorNamespace,
							testConfig.dash0Config,
							logger,
						)
						if testConfig.expectedError != "" {
							Expect(err).To(HaveOccurred())
							Expect(err.Error()).To(ContainSubstring(testConfig.expectedError))
						} else {
							Expect(err).ToNot(HaveOccurred())
							Expect(token).NotTo(BeNil())
							Expect(*token).To(Equal(testConfig.expectedToken))
						}
					},
					Entry(
						"should error if the Dash0 config has no secret ref", exchangeTestConfig{
							dash0Config:   dash0common.Dash0Configuration{},
							expectedError: "dash0Config has no secret ref",
						},
					),
					Entry(
						"should error if the secret does not exist", exchangeTestConfig{
							dash0Config: dash0common.Dash0Configuration{
								Endpoint:    EndpointDash0Test,
								ApiEndpoint: ApiEndpointTest,
								Authorization: dash0common.Authorization{
									SecretRef: &SecretRefTest,
								},
							},
							expectedError: "failed to fetch secret with name secret-ref in namespace test-operator-namespace for Dash0 self-monitoring/API access: secrets \"secret-ref\" not found",
						},
					),
					Entry(
						"should error if the secret exists but does not have the key", exchangeTestConfig{
							dash0Config: dash0common.Dash0Configuration{
								Endpoint:    EndpointDash0Test,
								ApiEndpoint: ApiEndpointTest,
								Authorization: dash0common.Authorization{
									SecretRef: &SecretRefTest,
								},
							},
							secret: &corev1.Secret{
								ObjectMeta: metav1.ObjectMeta{
									Namespace: OperatorNamespace,
									Name:      "secret-ref",
								},
								Data: map[string][]byte{
									"wrong-key": []byte("value"),
								},
							},
							expectedError: "secret \"test-operator-namespace/secret-ref\" does not contain key \"key\"",
						},
					),
					Entry(
						"should resolve the secret ref and return the token", exchangeTestConfig{
							dash0Config: dash0common.Dash0Configuration{
								Endpoint:    EndpointDash0Test,
								ApiEndpoint: ApiEndpointTest,
								Authorization: dash0common.Authorization{
									SecretRef: &SecretRefTest,
								},
							},
							secret:        DefaultSecret(),
							expectedToken: AuthorizationTokenTestFromSecret,
						},
					),
				)
			},
		)
	},
)

func createSelfMonitoringConfiguration(export *dash0common.Export) SelfMonitoringConfiguration {
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: true,
		Export:                *export,
	}
}

func expectedMetricsPipeline(exporterSuffix string) string {
	return `
    metrics:
      level: detailed
      views:
        # this metric was added in 0.145.0 and has a high cardinality due to its pod_identifier attribute
        - selector:
            instrument_name: "otelcol.k8s.pod.association"
          stream:
            aggregation:
              drop: {}
        # the metrics below are not directly related to the issue, but have been added by enabling 'level: detailed',
        # which is required to use views
        - selector:
            instrument_name: "http.client.*"
          stream:
            aggregation:
              drop: {}
        - selector:
            instrument_name: "http.server.*"
          stream:
            aggregation:
              drop: {}
        - selector:
            instrument_name: "rpc.*"
          stream:
            aggregation:
              drop: {}
        - selector:
            instrument_name: "otelcol_processor_batch_batch_send_size_bytes"
          stream:
            aggregation:
              drop: {}
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:` + exporterSuffix
}

var _ = Describe("redacting the self-monitoring configuration for logging", func() {

	It("redacts the resolved token, the literal export token and resolved secret header values", func() {
		literalToken := AuthorizationTokenTest
		resolvedToken := AuthorizationTokenTest
		cfg := SelfMonitoringConfiguration{
			SelfMonitoringEnabled: true,
			Export: dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint:      EndpointDash0Test,
					Dataset:       DatasetCustomTest,
					Authorization: dash0common.Authorization{Token: &literalToken},
				},
			},
			Token:                      &resolvedToken,
			ResolvedSecretHeaderValues: map[string]string{"Authorization": "Bearer secret"},
		}

		redacted := cfg.RedactedForLogging()

		Expect(redacted.SelfMonitoringEnabled).To(BeTrue())
		Expect(*redacted.Token).To(Equal(dash0common.RedactedValue))
		Expect(*redacted.Export.Dash0.Authorization.Token).To(Equal(dash0common.RedactedValue))
		Expect(redacted.Export.Dash0.Endpoint).To(Equal(EndpointDash0Test))
		Expect(redacted.Export.Dash0.Dataset).To(Equal(DatasetCustomTest))
		Expect(redacted.ResolvedSecretHeaderValues["Authorization"]).To(Equal(dash0common.RedactedValue))

		// the original configuration must not be mutated
		Expect(*cfg.Token).To(Equal(AuthorizationTokenTest))
		Expect(*cfg.Export.Dash0.Authorization.Token).To(Equal(AuthorizationTokenTest))
		Expect(cfg.ResolvedSecretHeaderValues["Authorization"]).To(Equal("Bearer secret"))
	})

	It("leaves a secret-ref-only authorization untouched and does not add a redacted token", func() {
		secretRef := SecretRefTest
		cfg := SelfMonitoringConfiguration{
			Export: dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint:      EndpointDash0Test,
					Authorization: dash0common.Authorization{SecretRef: &secretRef},
				},
			},
		}

		redacted := cfg.RedactedForLogging()

		Expect(redacted.Token).To(BeNil())
		Expect(redacted.Export.Dash0.Authorization.Token).To(BeNil())
		Expect(redacted.Export.Dash0.Authorization.SecretRef).ToNot(BeNil())
		Expect(redacted.Export.Dash0.Authorization.SecretRef.Name).To(Equal(SecretRefTest.Name))
	})
})

var _ = Describe("redacting the OTel SDK config for logging", func() {

	It("redacts all header values while preserving header names and non-header fields", func() {
		cfg := &common.OTelSdkConfig{
			Endpoint: EndpointDash0Test,
			Protocol: common.ProtocolGrpc,
			Headers: map[string]string{
				util.AuthorizationHeaderName: "Bearer secret-token",
				util.Dash0DatasetHeaderName:  DatasetCustomTest,
			},
		}

		redacted := redactOTelSdkConfigForLogging(cfg)

		Expect(redacted.Endpoint).To(Equal(EndpointDash0Test))
		Expect(redacted.Protocol).To(Equal(common.ProtocolGrpc))
		Expect(redacted.Headers[util.AuthorizationHeaderName]).To(Equal(dash0common.RedactedValue))
		Expect(redacted.Headers[util.Dash0DatasetHeaderName]).To(Equal(dash0common.RedactedValue))

		// the original config must not be mutated
		Expect(cfg.Headers[util.AuthorizationHeaderName]).To(Equal("Bearer secret-token"))
		Expect(cfg.Headers[util.Dash0DatasetHeaderName]).To(Equal(DatasetCustomTest))
	})

	It("returns nil for a nil config", func() {
		Expect(redactOTelSdkConfigForLogging(nil)).To(BeNil())
	})
})
