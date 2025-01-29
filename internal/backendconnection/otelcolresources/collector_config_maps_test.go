// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type testConfig struct {
	assembleConfigMapFunction func(*oTelColConfig, []string, bool) (*corev1.ConfigMap, error)
	pipelineNames             []string
}

const (
	GrpcEndpointTest = "example.com:4317"
	HttpEndpointTest = "https://example.com:4318"
)

var (
	bearerWithAuthToken     = fmt.Sprintf("Bearer ${env:%s}", authTokenEnvVarName)
	sequenceOfMappingsRegex = regexp.MustCompile(`^([\w-]+)=([\w-]+)$`)
	sequenceIndexRegex      = regexp.MustCompile(`^(\d+)$`)
	monitoredNamespaces     = []string{"namespace-1", "namespace-2"}
)

var _ = Describe("The OpenTelemetry Collector ConfigMaps", func() {

	testConfigs := []TableEntry{
		Entry(
			"for the DaemonSet",
			testConfig{
				assembleConfigMapFunction: assembleDaemonSetCollectorConfigMapWithoutScrapingNamespaces,
				pipelineNames: []string{
					"traces/downstream",
					"metrics/downstream",
					"logs/downstream",
				},
			}),
		Entry(
			"for the Deployment",
			testConfig{
				assembleConfigMapFunction: assembleDeploymentCollectorConfigMapForTest,
				pipelineNames: []string{
					"metrics/downstream",
				},
			}),
	}

	Describe("renders exporters", func() {

		DescribeTable("should fail if no exporter is configured", func(testConfig testConfig) {
			_, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     dash0v1alpha1.Export{},
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")))

		}, testConfigs)

		DescribeTable("should render the Dash0 exporter without other exporters, with default settings", func(testConfig testConfig) {
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
			}, monitoredNamespaces, false)

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
			}, monitoredNamespaces, false)

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

		DescribeTable("should render a debug exporter in development mode", func(testConfig testConfig) {
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
			}, monitoredNamespaces, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(2))

			debugExporterRaw := exporters["debug"]
			Expect(debugExporterRaw).ToNot(BeNil())
			debugExporter := debugExporterRaw.(map[string]interface{})
			Expect(debugExporter).To(HaveLen(0))

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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)

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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)

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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)
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
			}, monitoredNamespaces, false)
			Expect(err).ToNot(HaveOccurred())

			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(4))

			debugExporterRaw := exporters["debug"]
			Expect(debugExporterRaw).ToNot(BeNil())
			debugExporter := debugExporterRaw.(map[string]interface{})
			Expect(debugExporter).To(HaveLen(0))

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

	DescribeTable("should not render resource processor if the cluster name has not been set", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
		}, monitoredNamespaces, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := readFromMap(collectorConfig, []string{"processors", "resource"})
		Expect(resourceProcessor).To(BeNil())
		verifyProcessorDoesNotAppearInAnyPipeline(collectorConfig, "resource")
		selfMonitoringTelemetryResource := readFromMap(
			collectorConfig,
			[]string{
				"service",
				"telemetry",
				"resource",
			})
		Expect(selfMonitoringTelemetryResource).To(BeNil())
	}, testConfigs)

	DescribeTable("should render resource processor with k8s.cluster.name if available", func(testConfig testConfig) {
		configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
			Namespace:   namespace,
			NamePrefix:  namePrefix,
			Export:      Dash0ExportWithEndpointAndToken(),
			ClusterName: "cluster-name",
		}, monitoredNamespaces, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := readFromMap(collectorConfig, []string{"processors", "resource"})
		Expect(resourceProcessor).ToNot(BeNil())
		attributes := readFromMap(resourceProcessor, []string{"attributes"})
		Expect(attributes).To(HaveLen(1))
		attrs := attributes.([]interface{})
		Expect(attrs[0].(map[string]interface{})["key"]).To(Equal("k8s.cluster.name"))
		Expect(attrs[0].(map[string]interface{})["value"]).To(Equal("cluster-name"))
		Expect(attrs[0].(map[string]interface{})["action"]).To(Equal("insert"))
		selfMonitoringTelemetryResource := readFromMap(
			collectorConfig,
			[]string{
				"service",
				"telemetry",
				"resource",
			})
		Expect(selfMonitoringTelemetryResource).ToNot(BeNil())
		Expect(selfMonitoringTelemetryResource.(map[string]interface{})["k8s.cluster.name"]).To(Equal("cluster-name"))
	}, testConfigs)

	Describe("should enable/disable kubernetes infrastructure metrics collection", func() {
		It("should not render the kubeletstats receiver if kubernetes infrastructure metrics collection is disabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
				KubernetesInfrastructureMetricsCollectionEnabled: false,
			}, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiver := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiver).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).ToNot(ContainElement("kubeletstats"))
		})

		It("should render the kubeletstats receiver if kubernetes infrastructure metrics collection is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiverRaw := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiverRaw).ToNot(BeNil())
			kubeletstatsReceiver := kubeletstatsReceiverRaw.(map[string]interface{})
			insecureSkipVerifyPropertyValue, hasInsecureSkipVerifyProperty := kubeletstatsReceiver["insecure_skip_verify"]
			Expect(hasInsecureSkipVerifyProperty).To(BeTrue())
			Expect(insecureSkipVerifyPropertyValue).To(Equal("${env:KUBELET_STATS_TLS_INSECURE}"))

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).To(ContainElement("kubeletstats"))
		})
	})

	Describe("filters metrics by namespace", func() {

		type ottlFilterExpressionTestConfig struct {
			monitoredNamespaces []string
			expectedExpression  string
		}

		DescribeTable("should render the namespace filter ottl expression", func(testConfig ottlFilterExpressionTestConfig) {
		}, []TableEntry{
			Entry("with nil", ottlFilterExpressionTestConfig{
				monitoredNamespaces: nil,
				expectedExpression:  "resource.attributes[\"k8s.namespace.name\"] != nil\n",
			}),
			Entry("with an empty slice", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{},
				expectedExpression:  "resource.attributes[\"k8s.namespace.name\"] != nil\n",
			}),
			Entry("with one namespace", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{"namespace-1"},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n",
			}),
			Entry("with two namespaces", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{"namespace-1", "namespace-2"},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-2\"\n",
			}),
			Entry("with three namespaces", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{"namespace-1", "namespace-2", "namespace-3"},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-2\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-3\"\n",
			}),
		})

		DescribeTable("should render the namespace filter and add it to the metrics pipeline", func(testConfig testConfig) {
			configMap, err := testConfig.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
			}, monitoredNamespaces, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			filterProcessor := readFromMap(collectorConfig, []string{"processors", "filter/metrics_only_monitored_namespaces"})
			Expect(filterProcessor).ToNot(BeNil())
			filters := readFromMap(filterProcessor, []string{"metrics", "metric"})
			Expect(filters).To(HaveLen(1))
			filterString := filters.([]interface{})[0].(string)
			Expect(filterString).To(Equal(`resource.attributes["k8s.namespace.name"] != nil and resource.attributes["k8s.namespace.name"] != "namespace-1" and resource.attributes["k8s.namespace.name"] != "namespace-2"`))
			pipelines := readPipelines(collectorConfig)
			metricsProcessors := readPipelineProcessors(pipelines, "metrics/downstream")
			Expect(metricsProcessors).To(ContainElement("filter/metrics_only_monitored_namespaces"))
		}, testConfigs)
	})

	Describe("prometheus scraping config", func() {
		var config = &oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
		}

		It("should not render the prometheus scraping config if no namespaces have scraping enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "prometheus"})).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).ToNot(ContainElement("prometheus"))
		})

		It("should render the prometheus scraping config with all namespaces for which scraping is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace1", "namespace2"},
				[]string{"namespace1", "namespace2"},
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "prometheus"})).ToNot(BeNil())
			for _, jobName := range []string{"kubernetes-pods", "kubernetes-pods-slow"} {
				verifyScrapeJobHasNamespaces(collectorConfig, jobName)
			}

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).To(ContainElement("prometheus"))
		})
	})

	Describe("on an IPv4 or IPv6 cluster", func() {
		type ipVersionTestConfig struct {
			ipv6     bool
			expected string
		}

		DescribeTable("should render IPv4 addresses in an IPv4 cluster", func(testConfig *ipVersionTestConfig) {
			var config = &oTelColConfig{
				Namespace:     namespace,
				NamePrefix:    namePrefix,
				Export:        Dash0ExportWithEndpointAndToken(),
				IsIPv6Cluster: testConfig.ipv6,
			}

			expected := testConfig.expected
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			healthCheckEndpoint := readFromMap(collectorConfig, []string{"extensions", "health_check", "endpoint"})
			grpcOtlpEndpoint := readFromMap(collectorConfig, []string{"receivers", "otlp", "protocols", "grpc", "endpoint"})
			httpOtlpEndpoint := readFromMap(collectorConfig, []string{"receivers", "otlp", "protocols", "http", "endpoint"})
			selfMonitoringTelemetryEndpoint := readFromMap(
				collectorConfig,
				[]string{
					"service",
					"telemetry",
					"metrics",
					"readers",
					"0",
					"pull",
					"exporter",
					"prometheus",
					"host",
				})
			Expect(healthCheckEndpoint).To(Equal(fmt.Sprintf("%s:13133", expected)))
			Expect(grpcOtlpEndpoint).To(Equal(fmt.Sprintf("%s:4317", expected)))
			Expect(httpOtlpEndpoint).To(Equal(fmt.Sprintf("%s:4318", expected)))
			Expect(selfMonitoringTelemetryEndpoint).To(Equal(expected))

			configMap, err = assembleDeploymentCollectorConfigMap(config, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig = parseConfigMapContent(configMap)
			healthCheckEndpoint = readFromMap(collectorConfig, []string{"extensions", "health_check", "endpoint"})
			selfMonitoringTelemetryEndpoint = readFromMap(
				collectorConfig,
				[]string{
					"service",
					"telemetry",
					"metrics",
					"readers",
					"0",
					"pull",
					"exporter",
					"prometheus",
					"host",
				})
			Expect(healthCheckEndpoint).To(Equal(fmt.Sprintf("%s:13133", expected)))
			Expect(selfMonitoringTelemetryEndpoint).To(Equal(expected))
		}, []TableEntry{
			Entry("IPv4 cluster", &ipVersionTestConfig{
				ipv6:     false,
				expected: "${env:MY_POD_IP}",
			}),
			Entry("IPv6 cluster", &ipVersionTestConfig{
				ipv6:     true,
				expected: "[${env:MY_POD_IP}]",
			}),
		})
	})
})

func assembleDaemonSetCollectorConfigMapWithoutScrapingNamespaces(
	config *oTelColConfig,
	monitoredNamespaces []string,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDaemonSetCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil,
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMapForTest(
	config *oTelColConfig,
	monitoredNamespaces []string,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDeploymentCollectorConfigMap(
		config,
		monitoredNamespaces,
		forDeletion,
	)
}

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
	pipelines := readPipelines(collectorConfig)
	Expect(pipelines).ToNot(BeNil())
	for _, pipelineName := range testConfig.pipelineNames {
		exporters := readPipelineExporters(pipelines, pipelineName)
		Expect(exporters).To(HaveLen(len(expectedExporters)))
		Expect(exporters).To(ContainElements(expectedExporters))
	}
}

func verifyProcessorDoesNotAppearInAnyPipeline(
	collectorConfig map[string]interface{},
	processorName ...string,
) {
	pipelines := readPipelines(collectorConfig)
	Expect(pipelines).ToNot(BeNil())
	for pipelineName := range pipelines {
		Expect(pipelineName).ToNot(BeNil())
		processors := readPipelineProcessors(pipelines, pipelineName)
		Expect(processors).ToNot(ContainElements(processorName))
	}
}

func verifyScrapeJobHasNamespaces(collectorConfig map[string]interface{}, jobName string) {
	namespacesKubernetesPodsRaw :=
		readFromMap(
			collectorConfig,
			pathToScrapeJob(jobName),
		)
	namespacesKubernetesPods := namespacesKubernetesPodsRaw.([]interface{})
	Expect(namespacesKubernetesPods).To(ContainElements("namespace1", "namespace2"))
}

func pathToScrapeJob(jobName string) []string {
	return []string{"receivers",
		"prometheus",
		"config",
		"scrape_configs",
		fmt.Sprintf("job_name=%s", jobName),
		"kubernetes_sd_configs",
		"role=pod",
		"namespaces",
		"names",
	}
}

func readPipelines(collectorConfig map[string]interface{}) map[string]interface{} {
	return ((collectorConfig["service"]).(map[string]interface{})["pipelines"]).(map[string]interface{})
}

//nolint:unparam
func readPipelineReceivers(pipelines map[string]interface{}, pipelineName string) []interface{} {
	return readPipelineList(pipelines, pipelineName, "receivers")
}

func readPipelineProcessors(pipelines map[string]interface{}, pipelineName string) []interface{} {
	return readPipelineList(pipelines, pipelineName, "processors")
}

func readPipelineExporters(pipelines map[string]interface{}, pipelineName string) []interface{} {
	return readPipelineList(pipelines, pipelineName, "exporters")
}

func readPipelineList(pipelines map[string]interface{}, pipelineName string, listName string) []interface{} {
	pipelineRaw := pipelines[pipelineName]
	Expect(pipelineRaw).ToNot(BeNil())
	pipeline := pipelineRaw.(map[string]interface{})
	listRaw := pipeline[listName]
	Expect(listRaw).ToNot(BeNil())
	return listRaw.([]interface{})
}

func readFromMap(object interface{}, path []string) interface{} {
	key := path[0]
	var sub interface{}

	sequenceOfMappingsMatches := sequenceOfMappingsRegex.FindStringSubmatch(key)
	sequenceIndexMatches := sequenceIndexRegex.FindStringSubmatch(key)
	if len(sequenceOfMappingsMatches) > 0 {
		// assume we have a sequence of objects, read by equality comparison with an attribute

		attributeName := sequenceOfMappingsMatches[1]
		attributeValue := sequenceOfMappingsMatches[2]

		s, isSlice := object.([]interface{})
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key %s, got %T", key, object))
		for _, item := range s {
			m, isMapInSlice := item.(map[string]interface{})
			Expect(isMapInSlice).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when checking an item in the slice read via key %s, got %T", key, object))
			val := m[attributeName]
			if val == attributeValue {
				sub = item
				break
			}
		}
	} else if len(sequenceIndexMatches) > 0 {
		// assume we have an indexed sequence, read by index
		indexRaw := sequenceIndexMatches[1]
		index, err := strconv.Atoi(indexRaw)
		Expect(err).ToNot(HaveOccurred())
		s, isSlice := object.([]interface{})
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key %s, got %T", key, object))
		Expect(len(s) > index).To(BeTrue())
		sub = s[index]
	} else {
		// assume we have a regular map, read by key
		m, isMap := object.(map[string]interface{})
		Expect(isMap).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when reading key %s, got %T", key, object))
		sub = m[key]
	}

	if len(path) == 1 {
		return sub
	}
	Expect(sub).ToNot(BeNil())
	return readFromMap(sub, path[1:])
}
