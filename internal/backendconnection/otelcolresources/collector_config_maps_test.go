// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type testConfig struct {
	assembleConfigMapFunction func(*oTelColConfig, bool) (*corev1.ConfigMap, error)
	pipelineNames             []string
}

const (
	GrpcEndpointTest = "example.com:4317"
	HttpEndpointTest = "https://example.com:4318"
)

var (
	bearerWithAuthToken = fmt.Sprintf("Bearer ${env:%s}", authTokenEnvVarName)
)

var _ = Describe("The OpenTelemetry Collector ConfigMap conent", func() {

	testConfigs := []TableEntry{
		Entry(
			"for the DaemonSet",
			testConfig{
				assembleConfigMapFunction: assembleDaemonSetCollectorConfigMap,
				pipelineNames: []string{
					"traces/downstream",
					"metrics/downstream",
					"logs/downstream",
				},
			}),
		Entry(
			"for the Deployment",
			testConfig{
				assembleConfigMapFunction: assembleDeploymentCollectorConfigMap,
				pipelineNames: []string{
					"metrics/downstream",
				},
			}),
	}

	DescribeTable("should fail if no exporter is configured", func(testConfig testConfig) {
		_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     dash0v1alpha1.Export{},
		}, false)
		Expect(err).To(HaveOccurred())
	}, testConfigs)

	DescribeTable("should fail to render the Dash0 exporter when no endpoint is provided", func(testConfig testConfig) {
		_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		}, false)
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")))

	}, testConfigs)

	DescribeTable("should render the Dash0 exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		}, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(1))

		exporter := exporters["otlp/dash0"]
		Expect(exporter).ToNot(BeNil())
		dash0OtlpExporter := exporter.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(headers[util.Dash0DatasetHeaderName]).To(BeNil())
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/dash0")
	}, testConfigs)

	DescribeTable("should render the Dash0 exporter with custom dataset", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Dataset:  "custom-dataset",
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		}, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(1))

		exporter := exporters["otlp/dash0"]
		Expect(exporter).ToNot(BeNil())
		dash0OtlpExporter := exporter.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(headers[util.Dash0DatasetHeaderName]).To(Equal("custom-dataset"))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/dash0")
	}, testConfigs)

	DescribeTable("should render a verbose debug exporter in development mode", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
			DevelopmentMode: true,
		}, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))

		debugExporterRaw := exporters["debug"]
		Expect(debugExporterRaw).ToNot(BeNil())
		debugExporter := debugExporterRaw.(map[string]interface{})
		Expect(debugExporter["verbosity"]).To(Equal("detailed"))

		exporter := exporters["otlp/dash0"]
		Expect(exporter).ToNot(BeNil())
		dash0OtlpExporter := exporter.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(headers[util.Dash0DatasetHeaderName]).To(BeNil())
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "debug", "otlp/dash0")
	}, testConfigs)

	DescribeTable("should fail to render a gRPC exporter when no endpoint is provided", func(testConfig testConfig) {
		_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
			},
		}, false)
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")))

	}, testConfigs)

	DescribeTable("should render an arbitrary gRPC exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: GrpcEndpointTest,
					Headers: []dash0v1alpha1.Header{
						{
							Name:  "Key1",
							Value: "Value1",
						},
						{
							Name:  "Key2",
							Value: "Value2",
						},
					},
				},
			},
		}, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(1))

		exporter2 := exporters["otlp/grpc"]
		Expect(exporter2).ToNot(BeNil())
		otlpGrpcExporter := exporter2.(map[string]interface{})
		Expect(otlpGrpcExporter).ToNot(BeNil())
		Expect(otlpGrpcExporter["endpoint"]).To(Equal(GrpcEndpointTest))
		headersRaw := otlpGrpcExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(otlpGrpcExporter["encoding"]).To(BeNil())

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/grpc")
	}, testConfigs)

	DescribeTable("should fail to render an HTTP exporter when no endpoint is provided", func(testConfig testConfig) {
		_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Http: &dash0v1alpha1.HttpConfiguration{
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
					Encoding: dash0v1alpha1.Proto,
				},
			},
		}, false)
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")))
	}, testConfigs)

	DescribeTable("should fail to render an HTTP exporter when no encoding is provided", func(testConfig testConfig) {
		_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
			},
		}, false)
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")))

	}, testConfigs)

	DescribeTable("should render an arbitrary HTTP exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{
						{
							Name:  "Key1",
							Value: "Value1",
						},
						{
							Name:  "Key2",
							Value: "Value2",
						},
					},
					Encoding: dash0v1alpha1.Json,
				},
			},
		}, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(1))

		exporter2 := exporters["otlphttp/json"]
		Expect(exporter2).ToNot(BeNil())
		otlpHttpExporter := exporter2.(map[string]interface{})
		Expect(otlpHttpExporter).ToNot(BeNil())
		Expect(otlpHttpExporter["endpoint"]).To(Equal(HttpEndpointTest))
		headersRaw := otlpHttpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(otlpHttpExporter["encoding"]).To(Equal("json"))

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlphttp/json")
	}, testConfigs)

	DescribeTable("should render the Dash0 exporter together with a gRPC exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
			},
		}, false)
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))

		exporter2 := exporters["otlp/dash0"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlp/grpc"]
		Expect(exporter3).ToNot(BeNil())
		httpExporter := exporter3.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal(HttpEndpointTest))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(httpExporter["encoding"]).To(BeNil())

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/dash0", "otlp/grpc")
	}, testConfigs)

	DescribeTable("should render the Dash0 exporter together with an HTTP exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
					Encoding: dash0v1alpha1.Proto,
				},
			},
		}, false)
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))

		exporter2 := exporters["otlp/dash0"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlphttp/proto"]
		Expect(exporter3).ToNot(BeNil())
		httpExporter := exporter3.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal(HttpEndpointTest))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(httpExporter["encoding"]).To(Equal("proto"))

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/dash0", "otlphttp/proto")
	}, testConfigs)

	DescribeTable("should render a gRPC exporter together with an HTTP exporter", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: GrpcEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key2",
						Value: "Value2",
					}},
					Encoding: dash0v1alpha1.Proto,
				},
			},
		}, false)
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))

		exporter2 := exporters["otlp/grpc"]
		Expect(exporter2).ToNot(BeNil())
		grpcOtlpExporter := exporter2.(map[string]interface{})
		Expect(grpcOtlpExporter).ToNot(BeNil())
		Expect(grpcOtlpExporter["endpoint"]).To(Equal(GrpcEndpointTest))
		headersRaw := grpcOtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(grpcOtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlphttp/proto"]
		Expect(exporter3).ToNot(BeNil())
		httpExporter := exporter3.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal(HttpEndpointTest))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(httpExporter["encoding"]).To(Equal("proto"))

		verifyDownstreamExportersInPipelines(collectorConfig, testConfig, "otlp/grpc", "otlphttp/proto")
	}, testConfigs)

	DescribeTable("should render a combination of all three exporter types", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: GrpcEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: HttpEndpointTest,
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key2",
						Value: "Value2",
					}},
					Encoding: dash0v1alpha1.Json,
				},
			},
			DevelopmentMode: true,
		}, false)
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(4))

		debugExporterRaw := exporters["debug"]
		Expect(debugExporterRaw).ToNot(BeNil())
		debugExporter := debugExporterRaw.(map[string]interface{})
		Expect(debugExporter["verbosity"]).To(Equal("detailed"))

		exporter2 := exporters["otlp/dash0"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlp/grpc"]
		Expect(exporter3).ToNot(BeNil())
		grpcExporter := exporter3.(map[string]interface{})
		Expect(grpcExporter["endpoint"]).To(Equal(GrpcEndpointTest))
		headersRaw = grpcExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))

		exporter4 := exporters["otlphttp/json"]
		Expect(exporter4).ToNot(BeNil())
		httpExporter := exporter4.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal(HttpEndpointTest))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(httpExporter["encoding"]).To(Equal("json"))

		verifyDownstreamExportersInPipelines(
			collectorConfig,
			testConfig,
			"debug",
			"otlp/dash0",
			"otlp/grpc",
			"otlphttp/json",
		)
	}, testConfigs)
})

func parseConfigMapContent(configMap *corev1.ConfigMap) map[string]interface{} {
	configMapContent := configMap.Data["config.yaml"]
	configMapParsed := &map[string]interface{}{}
	err := yaml.Unmarshal([]byte(configMapContent), configMapParsed)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Cannot parse config map content:\n%s\n", configMapContent))
	return *configMapParsed
}

func verifyDownstreamExportersInPipelines(
	collectorConfig map[string]interface{},
	testConfig testConfig,
	expectedExporters ...string,
) {
	pipelines := ((collectorConfig["service"]).(map[string]interface{})["pipelines"]).(map[string]interface{})
	Expect(pipelines).ToNot(BeNil())
	for _, pipelineName := range testConfig.pipelineNames {
		pipeline := (pipelines[pipelineName]).(map[string]interface{})
		exporters := (pipeline["exporters"]).([]interface{})
		Expect(exporters).To(HaveLen(len(expectedExporters)))
		Expect(exporters).To(ContainElements(expectedExporters))
	}
}
