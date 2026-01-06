// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("self monitoring and API access", Ordered, func() {

	ctx := context.Background()
	logger := ptr.To(log.FromContext(ctx))

	Describe("convert operator configuration resource to self monitoring settings", func() {

		type resourceToSelfMonitoringTestConfig struct {
			operatorConfigurationSpec           *dash0v1alpha1.Dash0OperatorConfigurationSpec
			expectError                         bool
			expectedSelfMonitoringConfiguration SelfMonitoringConfiguration
		}

		DescribeTable("should convert the operator configuration resource to self monitoring configuration", func(testConfig resourceToSelfMonitoringTestConfig) {
			var operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration
			if testConfig.operatorConfigurationSpec != nil {
				operatorConfigurationResource = &dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec:       *testConfig.operatorConfigurationSpec,
				}
			}
			selfMonitoringConfiguration, err := ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
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
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(false)},
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
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         nil,
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
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         &dash0common.Export{},
					},
					expectError: true,
				},
			),
			Entry(
				"should convert Dash0 export with token",
				resourceToSelfMonitoringTestConfig{
					operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         Dash0ExportWithEndpointAndToken(),
					},
					expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *Dash0ExportWithEndpointAndToken(),
					},
				},
			),
			Entry(
				"should convert Dash0 export with secret ref",
				resourceToSelfMonitoringTestConfig{
					operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         Dash0ExportWithEndpointAndSecretRef(),
					},
					expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *Dash0ExportWithEndpointAndSecretRef(),
					},
				},
			),
			Entry(
				"should use custom dataset",
				resourceToSelfMonitoringTestConfig{
					operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         Dash0ExportWithEndpointTokenAndCustomDataset(),
					},
					expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *Dash0ExportWithEndpointTokenAndCustomDataset(),
					},
				},
			),
			Entry(
				"should ignore grpc and http exports if a Dash0 export is present",
				resourceToSelfMonitoringTestConfig{
					operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export: &dash0common.Export{
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
					expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *Dash0ExportWithEndpointAndToken(),
					},
				},
			),
			Entry(
				"should convert gRPC export",
				resourceToSelfMonitoringTestConfig{
					operatorConfigurationSpec: &dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         GrpcExportTest(),
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
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(true)},
						Export:         HttpExportTest(),
					},
					expectedSelfMonitoringConfiguration: SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *HttpExportTest(),
					},
				},
			),
		)
	})

	Describe("convert export settings to env vars", func() {

		type exportToEnvVarsTestConfig struct {
			export                     dash0common.Export
			expectedEndpointAndHeaders EndpointAndHeaders
		}

		DescribeTable("should convert the export self monitoring env vars", func(testConfig exportToEnvVarsTestConfig) {
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
						Headers: []dash0common.Header{{
							Name:  util.AuthorizationHeaderName,
							Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
						}},
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
						Headers: []dash0common.Header{{
							Name:  util.AuthorizationHeaderName,
							Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
						}},
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
						Headers: []dash0common.Header{{
							Name:  util.AuthorizationHeaderName,
							Value: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
						}},
					},
				},
			),
			Entry(
				"should convert gRPC export",
				exportToEnvVarsTestConfig{
					export: dash0common.Export{
						Grpc: &dash0common.GrpcConfiguration{
							Endpoint: EndpointGrpcWithProtocolTest,
							Headers: []dash0common.Header{{
								Name:  "Key",
								Value: "Value",
							}},
						},
					},
					expectedEndpointAndHeaders: EndpointAndHeaders{
						Endpoint: EndpointGrpcWithProtocolTest,
						Protocol: common.ProtocolGrpc,
						Headers: []dash0common.Header{{
							Name:  "Key",
							Value: "Value",
						}},
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
						Headers: []dash0common.Header{{
							Name:  "Key",
							Value: "Value",
						}},
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
						Headers: []dash0common.Header{{
							Name:  "Key",
							Value: "Value",
						}},
					},
				},
			),
			Entry(
				"should overide HTTP/json to HTTP/protobuf",
				exportToEnvVarsTestConfig{
					export: dash0common.Export{
						Http: &dash0common.HttpConfiguration{
							Endpoint: EndpointHttpTest,
							Headers: []dash0common.Header{{
								Name:  "Key",
								Value: "Value",
							}},
							Encoding: dash0common.Json,
						},
					},
					expectedEndpointAndHeaders: EndpointAndHeaders{
						Endpoint: EndpointHttpTest,
						Protocol: common.ProtocolHttpProtobuf,
						Headers: []dash0common.Header{{
							Name:  "Key",
							Value: "Value",
						}},
					},
				},
			),
		)

		type prependProtocolTestConfig struct {
			endpoint        string
			defaultProtocol string
			wanted          string
		}

		DescribeTable("should convert the export self monitoring env vars", func(testConfig prependProtocolTestConfig) {
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
	})

	Describe("convert export settings to collector metrics self-monitoring pipeline string", func() {

		type exportToCollectorMetricsSelfMonitoringPipelineTestConfig struct {
			selfMonitoringConfiguration   SelfMonitoringConfiguration
			expectedMetricsPipelineString string
		}

		var (
			dash0ExportExpectedMetricsPipelineString = `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`

			dash0ExportWithCustomDatasetExpectedMetricsPipelineString = `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: https://endpoint.dash0.com:4317
                headers:
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
                  Dash0-Dataset: "test-dataset"
`
		)

		DescribeTable("should convert the self monitoring configuration", func(testConfig exportToCollectorMetricsSelfMonitoringPipelineTestConfig) {
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
						}),
					expectedMetricsPipelineString: dash0ExportExpectedMetricsPipelineString,
				},
			),
			Entry(
				"should convert non-TLS Dash0 export",
				exportToCollectorMetricsSelfMonitoringPipelineTestConfig{
					selfMonitoringConfiguration: createSelfMonitoringConfiguration(&dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: "http://endpoint.dash0.com:4317",
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://endpoint.dash0.com:4317
                insecure: true
                headers:
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
`,
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
						}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
`,
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
						}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
`,
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
						}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://endpoint.backend.com:4317
                insecure: true
`,
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
						}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
`,
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
						}),
					expectedMetricsPipelineString: `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:
                protocol: http/json
                endpoint: https://endpoint.backend.com:4318
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
`,
				},
			),
		)
	})

	Describe("convert export settings to collector logs self-monitoring pipeline string", func() {

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
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
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
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
                  Dash0-Dataset: "test-dataset"
`
		)

		DescribeTable("should convert the self monitoring configuration", func(testConfig exportToCollectorLogsSelfMonitoringPipelineTestConfig) {
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
						}),
					expectedLogsPipelineString: dash0ExportExpectedLogsPipelineString,
				},
			),
			Entry(
				"should convert non-TLS Dash0 export",
				exportToCollectorLogsSelfMonitoringPipelineTestConfig{
					selfMonitoringConfiguration: createSelfMonitoringConfiguration(&dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: "http://endpoint.dash0.com:4317",
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					}),
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
                  Authorization: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"
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
						}),
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
						}),
					expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: grpc
                endpoint: dns://endpoint.backend.com:4317
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
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
						}),
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
						}),
					expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/protobuf
                endpoint: https://endpoint.backend.com:4318
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
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
						}),
					expectedLogsPipelineString: `
    logs:
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/json
                endpoint: https://endpoint.backend.com:4318
                headers:
                  Key1: "Value1"
                  Key2: "Value2"
                  KeyWithoutValue: ""
`,
				},
			),
		)
	})

	Describe("resolve secret ref to auth token", func() {
		type exchangeTestConfig struct {
			operatorConfiguration      *dash0v1alpha1.Dash0OperatorConfiguration
			secret                     *corev1.Secret
			expectedError              string
			expectAuthToken            string
			expectSetAuthTokenCalls    int
			expectRemoveAuthTokenCalls int
		}

		var (
			authTokenClient1 = &DummyAuthTokenClient{}
			authTokenClient2 = &DummyAuthTokenClient{}
			dummyClients     = []*DummyAuthTokenClient{
				authTokenClient1,
				authTokenClient2,
			}
			createdObjectsSelfMonitoringTest []client.Object
		)

		BeforeEach(func() {
			for _, c := range dummyClients {
				c.Reset()
			}
		})

		AfterEach(func() {
			createdObjectsSelfMonitoringTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsSelfMonitoringTest)
		})

		DescribeTable("should fetch and decode the auth token", func(testConfig exchangeTestConfig) {
			if testConfig.secret != nil {
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				Expect(k8sClient.Create(ctx, testConfig.secret)).To(Succeed())
				createdObjectsSelfMonitoringTest = append(createdObjectsSelfMonitoringTest, testConfig.secret)
			}
			err := ExchangeSecretRefForToken(
				ctx,
				k8sClient,
				[]AuthTokenClient{
					authTokenClient1,
					authTokenClient2,
				},
				OperatorNamespace,
				testConfig.operatorConfiguration,
				logger,
			)
			if testConfig.expectedError != "" {
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(testConfig.expectedError))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}

			for _, c := range dummyClients {
				Expect(c.AuthToken).To(Equal(testConfig.expectAuthToken))
				Expect(c.SetAuthTokenCalls).To(Equal(testConfig.expectSetAuthTokenCalls))
				Expect(c.RemoveAuthTokenCalls).To(Equal(testConfig.expectRemoveAuthTokenCalls))
			}
		},
			Entry("should error if the operator configuration is nil", exchangeTestConfig{
				operatorConfiguration:      nil,
				expectedError:              "operatorConfiguration is nil",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should error if the operator configuration has no export", exchangeTestConfig{
				operatorConfiguration:      &dash0v1alpha1.Dash0OperatorConfiguration{},
				expectedError:              "operatorConfiguration has no export",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should error if the operator configuration has no Dash0 export", exchangeTestConfig{
				operatorConfiguration: &dash0v1alpha1.Dash0OperatorConfiguration{
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: &dash0common.Export{},
					},
				},
				expectedError:              "operatorConfiguration has no Dash0 export",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should error if the operator configuration has no secret ref", exchangeTestConfig{
				operatorConfiguration: &dash0v1alpha1.Dash0OperatorConfiguration{
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: &dash0common.Export{
							Dash0: &dash0common.Dash0Configuration{},
						},
					},
				},
				expectedError:              "operatorConfiguration has no secret ref",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should error if the secret does not exist", exchangeTestConfig{
				operatorConfiguration: &dash0v1alpha1.Dash0OperatorConfiguration{
					Spec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				},
				expectedError:              "failed to fetch secret with name secret-ref in namespace test-operator-namespace for Dash0 self-monitoring/API access: secrets \"secret-ref\" not found",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should error if the secret exists but does not have the key", exchangeTestConfig{
				operatorConfiguration: &dash0v1alpha1.Dash0OperatorConfiguration{
					Spec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
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
				expectedError:              "secret \"test-operator-namespace/secret-ref\" does not contain key \"key\"",
				expectRemoveAuthTokenCalls: 1,
			}),
			Entry("should distribute the resolved auth token to all clients", exchangeTestConfig{
				operatorConfiguration: &dash0v1alpha1.Dash0OperatorConfiguration{
					Spec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				},
				secret:                     DefaultSecret(),
				expectAuthToken:            AuthorizationTokenTestFromSecret,
				expectSetAuthTokenCalls:    1,
				expectRemoveAuthTokenCalls: 0,
			}),
		)
	})
})

func createSelfMonitoringConfiguration(export *dash0common.Export) SelfMonitoringConfiguration {
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: true,
		Export:                *export,
	}
}
