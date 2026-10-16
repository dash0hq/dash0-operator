// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
						"should keep the insecure flag of a gRPC export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint: EndpointGrpcTest,
											Insecure: new(true),
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export: dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Insecure: new(true),
									},
								},
							},
						},
					),
					Entry(
						"should keep insecureSkipVerify of a gRPC export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Grpc: &dash0common.GrpcConfiguration{
											Endpoint:           EndpointGrpcTest,
											InsecureSkipVerify: new(true),
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export: dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint:           EndpointGrpcTest,
										InsecureSkipVerify: new(true),
									},
								},
							},
						},
					),
					Entry(
						"should keep insecureSkipVerify of an HTTP export",
						resourceToSelfMonitoringTestConfig{
							operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
								SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
								Exports: []dash0common.Export{
									{
										Http: &dash0common.HttpConfiguration{
											Endpoint:           EndpointHttpTest,
											Encoding:           dash0common.Proto,
											InsecureSkipVerify: new(true),
										},
									},
								},
							},
							expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
								SelfMonitoringEnabled: true,
								Export: dash0common.Export{
									Http: &dash0common.HttpConfiguration{
										Endpoint:           EndpointHttpTest,
										Encoding:           dash0common.Proto,
										InsecureSkipVerify: new(true),
									},
								},
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

				const (
					operatorManagerOnlyWarning    = "has a format that only the operator manager supports"
					missingSlashError             = "Probably a slash is missing, use dns:///" + EndpointGrpcTest + " instead"
					ignoredSkipVerifyWarning      = "sets insecureSkipVerify, which only the operator manager supports"
					noWarning                     = ""
					endpointWithUnsupportedScheme = "dns://dns-server/endpoint.backend.com:4317"
				)

				DescribeTable(
					"should warn about export settings that are not supported for self-monitoring",
					func(export dash0common.Export, expectedWarning string) {
						observedCore, observedLogs := observer.New(zapcore.WarnLevel)
						observedLogger := logd.NewLogger(zapr.NewLogger(zap.New(observedCore)))
						_, err := ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
							ctx,
							k8sClient,
							OperatorNamespace,
							&dash0v1alpha1.Dash0OperatorConfiguration{
								ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
								Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
									SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
									Exports:        []dash0common.Export{export},
								},
							},
							observedLogger,
						)
						Expect(err).ToNot(HaveOccurred())
						if expectedWarning == noWarning {
							Expect(observedLogs.Len()).To(Equal(0))
						} else {
							Expect(observedLogs.Len()).To(Equal(1))
							Expect(observedLogs.FilterMessageSnippet(expectedWarning).Len()).To(Equal(1))
						}
					},
					Entry("gRPC, no scheme",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: EndpointGrpcTest}},
						noWarning,
					),
					Entry("gRPC, https",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "https://" + EndpointGrpcTest}},
						noWarning,
					),
					Entry("gRPC, dns without authority",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns:///" + EndpointGrpcTest}},
						noWarning,
					),
					Entry("gRPC, dns with authority",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: endpointWithUnsupportedScheme}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, dns without slashes",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns:" + EndpointGrpcTest}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, upper case dns with authority",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "DNS://dns-server/" + EndpointGrpcTest,
						}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, dns without slashes with IP address",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns:10.0.0.1:4317"}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, dns without port",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "dns://dns-server/endpoint.backend.com",
						}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, passthrough",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "passthrough:///" + EndpointGrpcTest}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, passthrough with IP address",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "passthrough:10.0.0.1:4317"}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, passthrough without port",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "passthrough:///endpoint.backend.com",
						}},
						"the target address endpoint.backend.com is not of the form host:port, which passthrough requires",
					),
					Entry("gRPC, URL inside a dns target",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "dns://dns-server/https://" + EndpointGrpcTest,
						}},
						"the target address https://"+EndpointGrpcTest+" is not of the form host[:port]",
					),
					Entry("gRPC, dns with a missing slash",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns://" + EndpointGrpcTest}},
						missingSlashError,
					),
					Entry("gRPC, upper case dns with a missing slash and a trailing slash",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "DNS://" + EndpointGrpcTest + "/"}},
						missingSlashError,
					),
					Entry("gRPC, dns with a missing slash and insecureSkipVerify",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           "dns://" + EndpointGrpcTest,
							InsecureSkipVerify: new(true),
						}},
						missingSlashError,
					),
					Entry("gRPC, dns with authority and insecureSkipVerify",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           endpointWithUnsupportedScheme,
							InsecureSkipVerify: new(true),
						}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, dns without address",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns://"}},
						"cannot be used for self-monitoring telemetry: the target address is missing",
					),
					Entry("gRPC, unregistered scheme",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "grpc://" + EndpointGrpcTest}},
						"grpc is not a scheme of the gRPC resolvers dns, passthrough, unix or unix-abstract",
					),
					Entry("gRPC, unix",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "unix:///var/run/otlp.sock"}},
						"the unix sockets of an export are not reachable from the operator manager pod",
					),
					Entry("gRPC, unix-abstract",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "unix-abstract:otlp"}},
						"the unix sockets of an export are not reachable from the operator manager pod",
					),
					Entry("gRPC, passthrough without address",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "passthrough:///"}},
						"cannot be used for self-monitoring telemetry: the target address is missing",
					),
					Entry("gRPC, unregistered scheme without slashes",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "grpc:endpoint.backend.com"}},
						"grpc is not a scheme of the gRPC resolvers dns, passthrough, unix or unix-abstract",
					),
					Entry("gRPC, host with named port, which gRPC passes to the dns resolver",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "endpoint.backend.com:https"}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, dns with unknown named port",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "dns://dns-server/endpoint.backend.com:no-such-port",
						}},
						"the port no-such-port of the target address endpoint.backend.com:no-such-port is unknown",
					),
					Entry("gRPC, passthrough with unknown named port",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "passthrough:///endpoint.backend.com:no-such-port",
						}},
						"the port no-such-port of the target address endpoint.backend.com:no-such-port is unknown",
					),
					Entry("gRPC, dns with invalid DNS server",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint: "dns://dns-server:/" + EndpointGrpcTest,
						}},
						"the DNS server dns-server: is not of the form host[:port]",
					),
					Entry("gRPC, dns with invalid DNS server and IP address, for which gRPC does not use the DNS server",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns://dns-server:/10.0.0.1:4317"}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, named port with query, which gRPC strips",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "endpoint.backend.com:https?x"}},
						operatorManagerOnlyWarning,
					),
					Entry("gRPC, IPv6 address with zone, which gRPC cannot parse",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "fe80::1%eth0"}},
						"it cannot be parsed as a gRPC target",
					),
					Entry("gRPC, passthrough with unix socket",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "passthrough:///unix:4317"}},
						"the unix sockets of an export are not reachable from the operator manager pod",
					),
					Entry("gRPC, dns with a missing slash and without port",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "dns://endpoint.backend.com:"}},
						"cannot be used for self-monitoring telemetry: the target address is missing",
					),
					Entry("gRPC, unparsable endpoint",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "grpc://" + EndpointGrpcTest + "/%zz"}},
						"it cannot be parsed as a gRPC target",
					),
					Entry("gRPC, insecureSkipVerify without scheme",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           EndpointGrpcTest,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("gRPC, insecureSkipVerify with upper case https",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           "HTTPS://" + EndpointGrpcTest,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("gRPC, insecureSkipVerify with dns without authority",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           "dns:///" + EndpointGrpcTest,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("gRPC, insecureSkipVerify with insecure=true without scheme",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           EndpointGrpcTest,
							Insecure:           new(true),
							InsecureSkipVerify: new(true),
						}},
						noWarning,
					),
					Entry("gRPC, insecureSkipVerify with http",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           "http://" + EndpointGrpcTest,
							InsecureSkipVerify: new(true),
						}},
						noWarning,
					),
					Entry("gRPC, insecureSkipVerify=false",
						dash0common.Export{Grpc: &dash0common.GrpcConfiguration{
							Endpoint:           EndpointGrpcTest,
							InsecureSkipVerify: new(false),
						}},
						noWarning,
					),
					Entry("HTTP, insecureSkipVerify with https",
						dash0common.Export{Http: &dash0common.HttpConfiguration{
							Endpoint:           EndpointHttpTest,
							Encoding:           dash0common.Proto,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("HTTP, insecureSkipVerify with http",
						dash0common.Export{Http: &dash0common.HttpConfiguration{
							Endpoint:           "http://endpoint.backend.com:4318",
							Encoding:           dash0common.Proto,
							InsecureSkipVerify: new(true),
						}},
						noWarning,
					),
					Entry("HTTP, insecureSkipVerify with upper case https",
						dash0common.Export{Http: &dash0common.HttpConfiguration{
							Endpoint:           "HTTPS://endpoint.backend.com:4318",
							Encoding:           dash0common.Proto,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("HTTP, insecureSkipVerify without scheme",
						dash0common.Export{Http: &dash0common.HttpConfiguration{
							Endpoint:           "endpoint.backend.com:4318",
							Encoding:           dash0common.Proto,
							InsecureSkipVerify: new(true),
						}},
						ignoredSkipVerifyWarning,
					),
					Entry("HTTP, insecureSkipVerify with upper case http",
						dash0common.Export{Http: &dash0common.HttpConfiguration{
							Endpoint:           "HTTP://endpoint.backend.com:4318",
							Encoding:           dash0common.Proto,
							InsecureSkipVerify: new(true),
						}},
						noWarning,
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
						"should send the default dataset if it has been set explicitly",
						exportToEnvVarsTestConfig{
							export: *Dash0ExportWithEndpointTokenAndExplicitDefaultDataset(),
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
										Value: util.DatasetDefault,
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
						"should convert insecure gRPC export and prepend http protocol",
						exportToEnvVarsTestConfig{
							export: dash0common.Export{
								Grpc: &dash0common.GrpcConfiguration{
									Endpoint: EndpointGrpcTest,
									Insecure: new(true),
								},
							},
							expectedEndpointAndHeaders: EndpointAndHeaders{
								Endpoint: "http://" + EndpointGrpcTest,
								Protocol: common.ProtocolGrpc,
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

				type grpcSelfMonitoringEndpointTestConfig struct {
					endpoint          string
					insecure          *bool
					wanted            string
					wantedUnsupported bool
				}

				DescribeTable(
					"should render the gRPC self-monitoring endpoint",
					func(testConfig grpcSelfMonitoringEndpointTestConfig) {
						result := grpcSelfMonitoringEndpoint(&dash0common.GrpcConfiguration{
							Endpoint: testConfig.endpoint,
							Insecure: testConfig.insecure,
						})
						Expect(result).To(Equal(testConfig.wanted))
						Expect(hasUnsupportedGrpcSelfMonitoringScheme(testConfig.endpoint)).To(
							Equal(testConfig.wantedUnsupported))
					},
					Entry("no scheme", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "endpoint.backend.com:4317",
						wanted:   "https://endpoint.backend.com:4317",
					}),
					Entry("no scheme, insecure=false", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "endpoint.backend.com:4317",
						insecure: new(false),
						wanted:   "https://endpoint.backend.com:4317",
					}),
					Entry("no scheme, insecure=true", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "endpoint.backend.com:4317",
						insecure: new(true),
						wanted:   "http://endpoint.backend.com:4317",
					}),
					Entry("IP address", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "10.0.0.1:4317",
						wanted:   "https://10.0.0.1:4317",
					}),
					Entry("http", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "http://endpoint.backend.com:4317",
						wanted:   "http://endpoint.backend.com:4317",
					}),
					Entry("https, insecure=true", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "HTTPS://endpoint.backend.com:4317",
						insecure: new(true),
						wanted:   "https://endpoint.backend.com:4317",
					}),
					Entry("http in upper case", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "HTTP://endpoint.backend.com:4317",
						wanted:   "http://endpoint.backend.com:4317",
					}),
					Entry("dns without authority", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "dns:///endpoint.backend.com:4317",
						wanted:   "https://endpoint.backend.com:4317",
					}),
					Entry("dns without authority, followed by an upper case scheme", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "dns:///HTTPS://endpoint.backend.com:4317",
						wanted:   "https://endpoint.backend.com:4317",
					}),
					Entry("dns without authority, insecure=true", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "DNS:///endpoint.backend.com:4317",
						insecure: new(true),
						wanted:   "http://endpoint.backend.com:4317",
					}),
					Entry("dns with authority", grpcSelfMonitoringEndpointTestConfig{
						endpoint:          "dns://dns-server/endpoint.backend.com:4317",
						wanted:            "dns://dns-server/endpoint.backend.com:4317",
						wantedUnsupported: true,
					}),
					Entry("unix with slashes", grpcSelfMonitoringEndpointTestConfig{
						endpoint:          "unix:///var/run/otlp.sock",
						wanted:            "unix:///var/run/otlp.sock",
						wantedUnsupported: true,
					}),
					Entry("unix without slashes", grpcSelfMonitoringEndpointTestConfig{
						endpoint:          "unix:otlp.sock",
						wanted:            "unix:otlp.sock",
						wantedUnsupported: true,
					}),
					Entry("dns with an IP address without slashes", grpcSelfMonitoringEndpointTestConfig{
						endpoint:          "dns:10.0.0.1:4317",
						wanted:            "dns:10.0.0.1:4317",
						wantedUnsupported: true,
					}),
					Entry("dns without authority, host named like a scheme", grpcSelfMonitoringEndpointTestConfig{
						endpoint: "dns:///unix:4317",
						wanted:   "https://unix:4317",
					}),
				)

				DescribeTable(
					"should render the gRPC endpoint for the operator manager",
					func(endpoint string, insecure *bool, wanted string) {
						Expect(operatorManagerGrpcEndpoint(&dash0common.GrpcConfiguration{
							Endpoint: endpoint,
							Insecure: insecure,
						})).To(Equal(wanted))
					},
					Entry("no scheme", "endpoint.backend.com:4317", nil, "https://endpoint.backend.com:4317"),
					Entry("dns with a missing slash", "dns://endpoint.backend.com:4317", nil,
						"https://endpoint.backend.com:4317"),
					Entry("dns with a missing slash, insecure", "dns://endpoint.backend.com:4317", new(true),
						"http://endpoint.backend.com:4317"),
					Entry("dns with a missing slash, host named like a scheme", "dns://dns:4317", nil, "https://dns:4317"),
					Entry("dns with a missing slash, IPv6 address", "dns://[::1]:4317", nil, "https://[::1]:4317"),
					Entry("dns with authority", "dns://dns-server/endpoint.backend.com:4317", nil,
						"dns://dns-server/endpoint.backend.com:4317"),
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

				// Mimics the containers of a collector pod: the collector container itself, which is configured via
				// the collector configuration, and the configuration-reloader sidecar, which runs the OTel Go SDK and
				// does not carry the data-pipeline header env vars, so the self-monitoring path must inject its own.
				collectorContainers := func() []corev1.Container {
					return []corev1.Container{
						{Name: "opentelemetry-collector"},
						{Name: "configuration-reloader"},
					}
				}

				// verifyNoSdkEnvVars asserts that the container has none of the environment variables that only the
				// OTel Go SDK reads. The collector container is configured via the service::telemetry section of the
				// collector configuration instead.
				verifyNoSdkEnvVars := func(container corev1.Container, protocol string) {
					for _, envVarName := range []string{
						otelExporterOtlpEndpointEnvVarName,
						otelExporterOtlpProtocolEnvVarName,
						otelExporterOtlpHeadersEnvVarName,
						util.OtelResourceAttributesEnvVarName,
						exporters.HeaderSecretEnvVarName(protocol, "self_monitoring", 1),
					} {
						Expect(slices.IndexFunc(container.Env, matchEnvVar(envVarName))).To(
							Equal(-1),
							fmt.Sprintf("container %s should not have the env var %s", container.Name, envVarName),
						)
					}
				}

				It("should wire secret-backed headers for an HTTP export in the Go SDK containers", func() {
					containers := collectorContainers()
					err := enableSelfMonitoringInContainers(
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
						"opentelemetry-collector",
					)
					Expect(err).NotTo(HaveOccurred())
					verifyNoSdkEnvVars(containers[0], "HTTP")
					verifySecretBackedHeaderWiring(containers[1], "HTTP")
				})

				It("should wire secret-backed headers for a gRPC export in the Go SDK containers", func() {
					containers := collectorContainers()
					err := enableSelfMonitoringInContainers(
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
						"opentelemetry-collector",
					)
					Expect(err).NotTo(HaveOccurred())
					verifyNoSdkEnvVars(containers[0], "GRPC")
					verifySecretBackedHeaderWiring(containers[1], "GRPC")
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

					dash0ExportWithExplicitDefaultDatasetExpectedMetricsPipelineString = expectedMetricsPipeline(`
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  - name: Authorization
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
                  - name: Dash0-Dataset
                    value: "default"
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
						"should send the default dataset if it has been set explicitly",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								Dash0ExportWithEndpointTokenAndExplicitDefaultDataset(),
							),
							expectedMetricsPipelineString: dash0ExportWithExplicitDefaultDatasetExpectedMetricsPipelineString,
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
                endpoint: https://endpoint.backend.com:4317
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
                endpoint: https://endpoint.backend.com:4317
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
						"should convert insecure gRPC export without scheme",
						exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
							selfMonitoringConfiguration: createSelfMonitoringConfiguration(
								&dash0common.Export{
									Grpc: &dash0common.GrpcConfiguration{
										Endpoint: EndpointGrpcTest,
										Insecure: new(true),
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
                endpoint: https://endpoint.backend.com:4317
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
                endpoint: https://endpoint.backend.com:4317
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
                endpoint: https://endpoint.backend.com:4317
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
                endpoint: https://endpoint.backend.com:4317
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
