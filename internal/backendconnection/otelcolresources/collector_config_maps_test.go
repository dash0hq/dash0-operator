// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type configMapType string
type signalType string
type objectType string

const (
	configMapTypeDaemonSet  configMapType = "DaemonSet"
	configMapTypeDeployment configMapType = "Deployment"
	signalTypeTraces        signalType    = "traces"
	signalTypeMetrics       signalType    = "metrics"
	signalTypeLogs          signalType    = "logs"
	objectTypeSpan          objectType    = "span"
	objectTypeSpanEvent     objectType    = "spanevent"
	objectTypeMetric        objectType    = "metric"
	objectTypeDataPoint     objectType    = "datapoint"
	objectTypeLogRecord     objectType    = "log_record"
)

type configMapTypeDefinition struct {
	cmType                    configMapType
	assembleConfigMapFunction func(*oTelColConfig, []string, []NamespacedTelemetryFilter, bool) (*corev1.ConfigMap, error)
	exporterPipelineNames     []string
}

type conditionExpectationsPerNamespaceAndObjectType map[string]map[objectType][]string

type filterExpectations struct {
	signalsWithFilters    []signalType
	conditions            conditionExpectationsPerNamespaceAndObjectType
	signalsWithoutFilters []signalType
}

type telemetryFilterTestConfigExpectations struct {
	namespaces []string
	daemonset  filterExpectations
	deployment filterExpectations
}

type telemetryFilterTestConfig struct {
	configMapTypeDefinition
	telemetryFilters []NamespacedTelemetryFilter
	expectations     telemetryFilterTestConfigExpectations
}

const (
	GrpcEndpointTest = "example.com:4317"
	HttpEndpointTest = "https://example.com:4318"
	namespace1       = "namespace-1"
	namespace2       = "namespace-2"
	namespace3       = "namespace-3"
)

var (
	bearerWithAuthToken     = fmt.Sprintf("Bearer ${env:%s}", authTokenEnvVarName)
	sequenceOfMappingsRegex = regexp.MustCompile(`^([\w-]+)=([\w-]+)$`)
	sequenceIndexRegex      = regexp.MustCompile(`^(\d+)$`)
	monitoredNamespaces     = []string{namespace1, namespace2}
)

var _ = Describe("The OpenTelemetry Collector ConfigMaps", func() {

	configMapTypeDefinitions := []configMapTypeDefinition{
		{
			cmType:                    configMapTypeDaemonSet,
			assembleConfigMapFunction: assembleDaemonSetCollectorConfigMapWithoutScrapingNamespaces,
			exporterPipelineNames: []string{
				"traces/downstream",
				"metrics/downstream",
				"logs/downstream",
			},
		},
		{
			cmType:                    configMapTypeDeployment,
			assembleConfigMapFunction: assembleDeploymentCollectorConfigMapForTest,
			exporterPipelineNames: []string{
				"metrics/downstream",
			},
		},
	}

	var daemonSetAndDeployment []TableEntry
	for _, cmTypeDef := range configMapTypeDefinitions {
		daemonSetAndDeployment = append(
			daemonSetAndDeployment,
			Entry(fmt.Sprintf("for the %s", string(cmTypeDef.cmType)), cmTypeDef))
	}

	Describe("renders exporters", func() {

		DescribeTable("should fail if no exporter is configured", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     dash0v1alpha1.Export{},
			}, monitoredNamespaces, nil, false)
			Expect(err).To(HaveOccurred())
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render the Dash0 exporter when no endpoint is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			}, monitoredNamespaces, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter without other exporters, with default settings", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
			}, monitoredNamespaces, nil, false)

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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/dash0")
		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter with custom dataset", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)

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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/dash0")
		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter with the insecure flag if there is an http:// prefix, for forwarding telemetry to another local collector", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: "HTTP://endpoint.dash0.com:1234",
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			}, monitoredNamespaces, nil, false)

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
			Expect(dash0OtlpExporter["endpoint"]).To(Equal("HTTP://endpoint.dash0.com:1234"))
			insecureFlag := readFromMap(dash0OtlpExporter, []string{"tls", "insecure"})
			Expect(insecureFlag).To(BeTrue())
			headersRaw := dash0OtlpExporter["headers"]
			Expect(headersRaw).ToNot(BeNil())
			headers := headersRaw.(map[string]interface{})
			Expect(headers).To(HaveLen(1))
			Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
			Expect(headers[util.Dash0DatasetHeaderName]).To(BeNil())
			Expect(dash0OtlpExporter["encoding"]).To(BeNil())

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/dash0")
		}, daemonSetAndDeployment)

		DescribeTable("should render a debug exporter in development mode", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:       namespace,
				NamePrefix:      namePrefix,
				Export:          Dash0ExportWithEndpointAndToken(),
				DevelopmentMode: true,
			}, monitoredNamespaces, nil, false)

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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "debug", "otlp/dash0")
		}, daemonSetAndDeployment)

		DescribeTable("should render a debug exporter with verbosity: detailed when requested", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:              namespace,
				NamePrefix:             namePrefix,
				Export:                 Dash0ExportWithEndpointAndToken(),
				DevelopmentMode:        false,
				DebugVerbosityDetailed: true,
			}, monitoredNamespaces, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(2))

			debugExporterRaw := exporters["debug"]
			Expect(debugExporterRaw).ToNot(BeNil())
			debugExporter := debugExporterRaw.(map[string]interface{})
			Expect(debugExporter).To(HaveLen(1))
			Expect(debugExporter["verbosity"]).To(Equal("detailed"))

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "debug", "otlp/dash0")
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render a gRPC exporter when no endpoint is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render an arbitrary gRPC exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)

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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/grpc")
		}, daemonSetAndDeployment)

		DescribeTable("should render a gRPC exporter with the insecure flag if there is an http:// prefix, for forwarding telemetry to another local collector", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export: dash0v1alpha1.Export{
					Grpc: &dash0v1alpha1.GrpcConfiguration{
						Endpoint: "http://example.com:1234",
					},
				},
			}, monitoredNamespaces, nil, false)

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
			Expect(otlpGrpcExporter["endpoint"]).To(Equal("http://example.com:1234"))
			insecureFlag := readFromMap(otlpGrpcExporter, []string{"tls", "insecure"})
			Expect(insecureFlag).To(BeTrue())
			headersRaw := otlpGrpcExporter["headers"]
			Expect(headersRaw).To(BeNil())
			Expect(otlpGrpcExporter["encoding"]).To(BeNil())

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/grpc")
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render an HTTP exporter when no endpoint is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")))
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render an HTTP exporter when no encoding is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render an arbitrary HTTP exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)

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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlphttp/json")
		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter together with a gRPC exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/dash0", "otlp/grpc")
		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter together with an HTTP exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/dash0", "otlphttp/proto")
		}, daemonSetAndDeployment)

		DescribeTable("should render a gRPC exporter together with an HTTP exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
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

			verifyDownstreamExportersInPipelines(collectorConfig, cmTypeDef, "otlp/grpc", "otlphttp/proto")
		}, daemonSetAndDeployment)

		DescribeTable("should render a combination of all three exporter types", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
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
			}, monitoredNamespaces, nil, false)
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
				cmTypeDef,
				"debug",
				"otlp/dash0",
				"otlp/grpc",
				"otlphttp/json",
			)
		}, daemonSetAndDeployment)
	})

	DescribeTable("should render batch processor with defaults if SendBatchMaxSize is not requested", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			Namespace:        namespace,
			NamePrefix:       namePrefix,
			Export:           Dash0ExportWithEndpointAndToken(),
			SendBatchMaxSize: nil,
		}, monitoredNamespaces, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		batchProcessorRaw := readFromMap(collectorConfig, []string{"processors", "batch"})
		Expect(batchProcessorRaw).ToNot(BeNil())
		batchProcessor := batchProcessorRaw.(map[string]interface{})
		Expect(batchProcessor).To(HaveLen(0))
	}, daemonSetAndDeployment)

	DescribeTable("should not set send_batch_max_size on batch processor if requested", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			Namespace:        namespace,
			NamePrefix:       namePrefix,
			Export:           Dash0ExportWithEndpointAndToken(),
			SendBatchMaxSize: ptr.To(uint32(16384)),
		}, monitoredNamespaces, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		batchProcessorRaw := readFromMap(collectorConfig, []string{"processors", "batch"})
		Expect(batchProcessorRaw).ToNot(BeNil())
		batchProcessor := batchProcessorRaw.(map[string]interface{})
		Expect(batchProcessor).To(HaveLen(1))
		sendBatchMaxSize := readFromMap(batchProcessor, []string{"send_batch_max_size"})
		Expect(sendBatchMaxSize).To(Equal(16384))
	}, daemonSetAndDeployment)

	DescribeTable("should not render resource processor if the cluster name has not been set", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
		}, monitoredNamespaces, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := readFromMap(collectorConfig, []string{"processors", "resource/clustername"})
		Expect(resourceProcessor).To(BeNil())
		verifyProcessorDoesNotAppearInAnyPipeline(collectorConfig, "resource/clustername")
		selfMonitoringTelemetryResource := readFromMap(
			collectorConfig,
			[]string{
				"service",
				"telemetry",
				"resource",
			})
		Expect(selfMonitoringTelemetryResource).To(BeNil())
	}, daemonSetAndDeployment)

	DescribeTable("should render resource processor with k8s.cluster.name if available", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			Namespace:   namespace,
			NamePrefix:  namePrefix,
			Export:      Dash0ExportWithEndpointAndToken(),
			ClusterName: "cluster-name",
		}, monitoredNamespaces, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := readFromMap(collectorConfig, []string{"processors", "resource/clustername"})
		Expect(resourceProcessor).ToNot(BeNil())
		attributes := readFromMap(resourceProcessor, []string{"attributes"})
		Expect(attributes).To(HaveLen(1))
		attrs := attributes.([]interface{})
		Expect(attrs[0].(map[string]interface{})["key"]).To(Equal("k8s.cluster.name"))
		Expect(attrs[0].(map[string]interface{})["value"]).To(Equal("cluster-name"))
		Expect(attrs[0].(map[string]interface{})["action"]).To(Equal("insert"))
		pipelines := readPipelines(collectorConfig)
		metricsProcessors := readPipelineProcessors(pipelines, "metrics/collect")
		Expect(metricsProcessors).ToNot(BeNil())
		Expect(metricsProcessors).To(ContainElement("resource/clustername"))
		selfMonitoringTelemetryResource := readFromMap(
			collectorConfig,
			[]string{
				"service",
				"telemetry",
				"resource",
			})
		Expect(selfMonitoringTelemetryResource).ToNot(BeNil())
		Expect(selfMonitoringTelemetryResource.(map[string]interface{})["k8s.cluster.name"]).To(Equal("cluster-name"))
	}, daemonSetAndDeployment)

	Describe("should enable/disable kubernetes infrastructure metrics collection and the hostmetrics receiver", func() {
		It("should not render the kubeletstats receiver and hostmetrics if kubernetes infrastructure metrics collection is disabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
				KubernetesInfrastructureMetricsCollectionEnabled: false,
				UseHostMetricsReceiver:                           false,
			}, nil, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiver := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiver).To(BeNil())
			hostmetricsReceiver := readFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
			Expect(hostmetricsReceiver).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/collect")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).ToNot(ContainElement("kubeletstats"))
			Expect(metricsReceivers).ToNot(ContainElement("hostmetrics"))
		})

		It("should render the kubeletstats and hostmetrics receiver if kubernetes infrastructure metrics collection is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				UseHostMetricsReceiver:                           true,
			}, nil, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiverRaw := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiverRaw).ToNot(BeNil())
			kubeletstatsReceiver := kubeletstatsReceiverRaw.(map[string]interface{})
			insecureSkipVerifyPropertyValue, hasInsecureSkipVerifyProperty := kubeletstatsReceiver["insecure_skip_verify"]
			Expect(hasInsecureSkipVerifyProperty).To(BeTrue())
			Expect(insecureSkipVerifyPropertyValue).To(Equal("${env:KUBELET_STATS_TLS_INSECURE}"))
			hostmetricsReceiver := readFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
			Expect(hostmetricsReceiver).ToNot(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/collect")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).To(ContainElement("kubeletstats"))
			Expect(metricsReceivers).To(ContainElement("hostmetrics"))
		})
	})

	Describe("filters metrics by namespace", func() {

		type ottlFilterExpressionTestConfig struct {
			monitoredNamespaces []string
			expectedExpression  string
		}

		DescribeTable("should render the namespace filter ottl expression", func(testConfig ottlFilterExpressionTestConfig) {
			expression := renderOttlNamespaceFilter(testConfig.monitoredNamespaces)
			Expect(expression).To(Equal(testConfig.expectedExpression))
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
				monitoredNamespaces: []string{namespace1},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n",
			}),
			Entry("with two namespaces", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{namespace1, namespace2},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-2\"\n",
			}),
			Entry("with three namespaces", ottlFilterExpressionTestConfig{
				monitoredNamespaces: []string{namespace1, namespace2, namespace3},
				expectedExpression: "resource.attributes[\"k8s.namespace.name\"] != nil\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-1\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-2\"\n" +
					"          and resource.attributes[\"k8s.namespace.name\"] != \"namespace-3\"\n",
			}),
		})

		DescribeTable("should render the namespace filter and add it to the metrics pipeline", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				Namespace:  namespace,
				NamePrefix: namePrefix,
				Export:     Dash0ExportWithEndpointAndToken(),
			}, monitoredNamespaces, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			filterProcessor := readFromMap(collectorConfig, []string{"processors", "filter/metrics/only_monitored_namespaces"})
			Expect(filterProcessor).ToNot(BeNil())
			filters := readFromMap(filterProcessor, []string{"metrics", "metric"})
			Expect(filters).To(HaveLen(1))
			filterString := filters.([]interface{})[0].(string)
			Expect(filterString).To(Equal(`resource.attributes["k8s.namespace.name"] != nil and resource.attributes["k8s.namespace.name"] != "namespace-1" and resource.attributes["k8s.namespace.name"] != "namespace-2"`))
			pipelines := readPipelines(collectorConfig)
			metricsProcessors := readPipelineProcessors(pipelines, "metrics/collect")
			Expect(metricsProcessors).ToNot(BeNil())
			Expect(metricsProcessors).To(ContainElement("filter/metrics/only_monitored_namespaces"))
		}, daemonSetAndDeployment)
	})

	Describe("prometheus scraping config", func() {
		var config = &oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
		}

		It("should not render the prometheus scraping config if no namespaces have scraping enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "prometheus"})).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/collect")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).ToNot(ContainElement("prometheus"))
		})

		It("should render the prometheus scraping config with all namespaces for which scraping is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace-1", "namespace-2"},
				[]string{"namespace-1", "namespace-2"},
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "prometheus"})).ToNot(BeNil())
			for _, jobName := range []string{
				"dash0-kubernetes-pods-scrape-config",
				"dash0-kubernetes-pods-scrape-config-slow",
			} {
				verifyScrapeJobHasNamespaces(collectorConfig, jobName)
			}

			pipelines := readPipelines(collectorConfig)
			prometheusMetricsReceivers := readPipelineReceivers(pipelines, "metrics/prometheus")
			Expect(prometheusMetricsReceivers).ToNot(BeNil())
			Expect(prometheusMetricsReceivers).To(ContainElement("prometheus"))
			prometheusMetricsProcessors := readPipelineProcessors(pipelines, "metrics/prometheus")
			Expect(prometheusMetricsProcessors).ToNot(BeNil())
			Expect(prometheusMetricsProcessors).To(ContainElement("transform/metrics/prometheus_service_attributes"))
			prometheusMetricsExporters := readPipelineExporters(pipelines, "metrics/prometheus")
			Expect(prometheusMetricsExporters).ToNot(BeNil())
			Expect(prometheusMetricsExporters).To(ContainElement("forward/metrics/prometheus"))

			downstreamMetricsReceivers := readPipelineReceivers(pipelines, "metrics/collect")
			Expect(downstreamMetricsReceivers).ToNot(BeNil())
			Expect(downstreamMetricsReceivers).To(ContainElement("forward/metrics/prometheus"))
		})
	})

	Describe("configurable filtering of telemetry per namespace", func() {
		// TODO tests for:
		// - empty list of monitoring resources
		// - on monitoring resource without filters
		// - on monitoring resource with filters
		// - multiple monitoring resource without filters
		// - multiple monitoring resource with filters
		// - multiple monitoring resource, some with and some without filters
		// - combinations of filters (spans, span events, metrics, data points, log records)

		var telemetryFilterTestConfigs []TableEntry
		for _, cmTypeDef := range configMapTypeDefinitions {
			telemetryFilterTestConfigs = slices.Concat(telemetryFilterTestConfigs, []TableEntry{

				Entry(fmt.Sprintf("[config map type: %s]: should filter traces/spans", cmTypeDef.cmType),
					createFilterTestForSingleObjectType(cmTypeDef,
						signalTypeTraces,
						objectTypeSpan,
						[]string{
							`attributes["http.route"] == "/ready"`,
							`attributes["http.route"] == "/metrics"`,
						},
						[]string{
							`attributes["express.type"] == "middleware"`,
							`attributes["express.type"] == "request_handler"`,
						},
					)),
				Entry(fmt.Sprintf("[config map type: %s]: should filter traces/span events", cmTypeDef.cmType),
					createFilterTestForSingleObjectType(cmTypeDef,
						signalTypeTraces,
						objectTypeSpanEvent,
						[]string{
							`attributes["grpc"] == true`,
							`IsMatch(name, ".*grpc.*")`,
						},
						[]string{
							`an ottl span event condition`,
							`another ottl span event condition`,
						},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should filter metrics/metrics", cmTypeDef.cmType),
					createFilterTestForSingleObjectType(cmTypeDef,
						signalTypeMetrics,
						objectTypeMetric,
						[]string{
							`name == "k8s.replicaset.available"`,
							`name == "k8s.replicaset.desired"`,
						},
						[]string{
							`type == METRIC_DATA_TYPE_HISTOGRAM`,
							`type == METRIC_DATA_TYPE_SUMMARY`,
						},
					)),
				Entry(fmt.Sprintf("[config map type: %s]: should filter metrics/data points", cmTypeDef.cmType),
					createFilterTestForSingleObjectType(cmTypeDef,
						signalTypeMetrics,
						objectTypeDataPoint,
						[]string{
							`metric.name == "some.metric" and value_int == 0`,
							`resource.attributes["service.name"] == "my_service_name"`,
						},
						[]string{
							`an ottl metric datapoint condition`,
							`another ottl metric datapoint condition`,
						},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should filter logs/log records", cmTypeDef.cmType),
					createFilterTestForSingleObjectType(cmTypeDef,
						signalTypeLogs,
						objectTypeLogRecord,
						[]string{
							`IsMatch(body, ".*password.*")`,
							`severity_number < SEVERITY_NUMBER_WARN`,
						},
						[]string{
							`an ottl log record condition`,
							`another ottl log record condition`,
						},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should filter traces in a subset of namespaces only", cmTypeDef.cmType),
					telemetryFilterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						telemetryFilters: []NamespacedTelemetryFilter{
							{
								Namespace: namespace1,
								TelemetryFilter: dash0v1alpha1.TelemetryFilter{
									Traces: &dash0v1alpha1.TraceFilter{
										SpanFilter: dash0v1alpha1.ObjectTypeFilter{
											Conditions: []string{"span condition 1", "span condition 2"},
										},
										SpanEventFilter: dash0v1alpha1.ObjectTypeFilter{
											Conditions: []string{"span event condition 1", "span event condition 2"},
										},
									},
								},
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: telemetryFilterTestConfigExpectations{
							namespaces: []string{namespace1},
							daemonset: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeTraces},
								signalsWithoutFilters: []signalType{signalTypeMetrics, signalTypeLogs},
								conditions: conditionExpectationsPerNamespaceAndObjectType{
									namespace1: {
										objectTypeSpan:      []string{"span condition 1", "span condition 2"},
										objectTypeSpanEvent: []string{"span event condition 1", "span event condition 2"},
									},
								},
							},
							deployment: emptyFilterExpectations(),
						},
					}),
				//
			})

		}

		DescribeTable("should filter telemetry", func(testConfig telemetryFilterTestConfig) {
			configMap, err := testConfig.assembleConfigMapFunction(
				&oTelColConfig{
					Namespace:  namespace,
					NamePrefix: namePrefix,
					Export:     Dash0ExportWithEndpointAndToken(),
				},
				monitoredNamespaces,
				testConfig.telemetryFilters,
				false,
			)
			Expect(err).ToNot(HaveOccurred())

			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			expectedNamespaces := testConfig.expectations.namespaces
			var expectations filterExpectations
			switch testConfig.cmType {
			case configMapTypeDaemonSet:
				expectations = testConfig.expectations.daemonset
			case configMapTypeDeployment:
				expectations = testConfig.expectations.deployment
			}

			for _, signal := range expectations.signalsWithFilters {
				routingConnectorRaw :=
					readFromMap(
						collectorConfig,
						[]string{"connectors", fmt.Sprintf("routing/%s/customfilter/send", signal)},
					)
				Expect(routingConnectorRaw).ToNot(BeNil())
				routingConnector := routingConnectorRaw.(map[string]interface{})
				Expect(routingConnector["default_pipelines"].([]interface{})[0]).To(
					Equal(fmt.Sprintf("%s/downstream", signal)))

				table := routingConnector["table"].([]interface{})
				Expect(table).To(HaveLen(len(expectedNamespaces)))
				for i, ns := range expectedNamespaces {
					route := table[i].(map[string]interface{})
					Expect(route["context"]).To(Equal("resource"))
					Expect(route["condition"]).To(Equal(fmt.Sprintf(`attributes["k8s.namespace.name"] == "%s"`, ns)))
					routePipelines := route["pipelines"].([]interface{})
					Expect(routePipelines).To(HaveLen(1))
					Expect(routePipelines[0]).To(Equal(fmt.Sprintf("%s/filter/%s", signal, ns)))
				}

				customFilterReturnConnector :=
					readFromMap(
						collectorConfig,
						[]string{"connectors", fmt.Sprintf("forward/%s/customfilter/return", signal)},
					)
				Expect(customFilterReturnConnector).ToNot(BeNil(),
					fmt.Sprintf("config map should have a custom filter return connector for the signal type %s", signal))

				for _, namespace := range expectedNamespaces {
					filterProcessorName := fmt.Sprintf("filter/%s/%s", signal, namespace)
					filterProcessorRaw := readFromMap(
						collectorConfig,
						[]string{"processors", filterProcessorName},
					)
					Expect(filterProcessorRaw).ToNot(BeNil(),
						fmt.Sprintf("expected filter processor %s to exist, but it didn't", filterProcessorName))
					filterProcessor := filterProcessorRaw.(map[string]interface{})
					Expect(filterProcessor["error_mode"]).To(Equal("ignore"))

					hasExpectedConditions := false
					expectedConditionsPerObjectType := expectations.conditions[namespace]
					for objectType, expectedConditions := range expectedConditionsPerObjectType {
						if len(expectedConditions) != 0 {
							hasExpectedConditions = true
						}
						filterConditionsRaw := readFromMap(filterProcessor, []string{string(signal), string(objectType)})
						Expect(filterConditionsRaw).ToNot(BeNil(),
							"expected %d filter conditions but there were none for signal \"%s\", namespace \"%s\", and object type \"%s\"",
							len(expectedConditions), signal, namespace, objectType)
						actualFilterConditions := filterConditionsRaw.([]interface{})
						Expect(actualFilterConditions).To(HaveLen(len(expectedConditions)))
						for i, expectedCondition := range expectedConditions {
							Expect(actualFilterConditions[i]).To(Equal(expectedCondition))
						}
					}

					if !hasExpectedConditions {
						Fail(
							fmt.Sprintf("expected conditions are empty for signal %s and namespace %s, although the signal should have filters?",
								signal, namespace))
					}

					collectionExporters := readPipelineExporters(pipelines, fmt.Sprintf("%s/collect", signal))
					Expect(collectionExporters).To(HaveLen(1))
					Expect(collectionExporters).To(ContainElement(fmt.Sprintf("routing/%s/customfilter/send", signal)))
				}

				for _, namespace := range expectedNamespaces {
					filterPipelineName := fmt.Sprintf("%s/filter/%s", signal, namespace)
					filterPipelineRaw :=
						readFromMap(pipelines, []string{filterPipelineName})
					Expect(filterPipelineRaw).ToNot(BeNil(),
						fmt.Sprintf("expected filter pipeline %s to exist, but it didn't", filterPipelineName))
					filterPipeline := filterPipelineRaw.(map[string]interface{})
					Expect(filterPipeline["receivers"].([]interface{})[0]).To(
						Equal(fmt.Sprintf("routing/%s/customfilter/send", signal)))
					Expect(filterPipeline["processors"].([]interface{})[0]).To(
						Equal(fmt.Sprintf("filter/%s/%s", signal, namespace)))
					Expect(filterPipeline["exporters"].([]interface{})[0]).To(
						Equal(fmt.Sprintf("forward/%s/customfilter/return", signal)))
				}
				// TODO Test that no other filter pipeline exist

				downstreamReceivers := readPipelineReceivers(pipelines, fmt.Sprintf("%s/downstream", signal))
				Expect(downstreamReceivers).To(HaveLen(2))
				Expect(downstreamReceivers).To(ContainElement(fmt.Sprintf("forward/%s/customfilter/return", signal)))
				Expect(downstreamReceivers).To(ContainElement(fmt.Sprintf("routing/%s/customfilter/send", signal)))
			}

			for _, signal := range expectations.signalsWithoutFilters {
				connectors := collectorConfig["connectors"]
				if testConfig.cmType == configMapTypeDeployment && connectors == nil {
					// The config map does not have any connectors, this can be valid for the deployment config map if
					// metrics aren't filtered.
					continue
				}
				routingConnector :=
					readFromMap(
						collectorConfig,
						[]string{"connectors", fmt.Sprintf("routing/%s/customfilter/send", signal)},
					)
				Expect(routingConnector).To(BeNil())
			}
		}, telemetryFilterTestConfigs)
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
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, nil, false)
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

			configMap, err = assembleDeploymentCollectorConfigMap(config, nil, nil, false)
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
	telemetryFilters []NamespacedTelemetryFilter,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDaemonSetCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil,
		telemetryFilters,
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMapForTest(
	config *oTelColConfig,
	monitoredNamespaces []string,
	telemetryFilters []NamespacedTelemetryFilter,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDeploymentCollectorConfigMap(
		config,
		monitoredNamespaces,
		telemetryFilters,
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
	cmTypeDef configMapTypeDefinition,
	expectedExporters ...string,
) {
	pipelines := readPipelines(collectorConfig)
	Expect(pipelines).ToNot(BeNil())
	for _, pipelineName := range cmTypeDef.exporterPipelineNames {
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
	Expect(namespacesKubernetesPods).To(ContainElements("namespace-1", "namespace-2"))
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
	Expect(pipelineRaw).ToNot(BeNil(), fmt.Sprintf("pipeline %s was nil", pipelineName))
	pipeline := pipelineRaw.(map[string]interface{})
	listRaw := pipeline[listName]
	if listRaw == nil {
		return nil
	}
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
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key \"%s\", got %T", key, object))
		for _, item := range s {
			m, isMapInSlice := item.(map[string]interface{})
			Expect(isMapInSlice).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when checking an item in the slice read via key \"%s\", got %T", key, object))
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
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key \"%s\", got %T", key, object))
		Expect(len(s) > index).To(BeTrue())
		sub = s[index]
	} else {
		// assume we have a regular map, read by key
		m, isMap := object.(map[string]interface{})
		Expect(isMap).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when reading key \"%s\", got %T", key, object))
		sub = m[key]
	}

	if len(path) == 1 {
		return sub
	}
	Expect(sub).ToNot(BeNil(), fmt.Sprintf("expected a nested element to be not nil when reading key \"%s\" in object %v", key, object))
	return readFromMap(sub, path[1:])
}

func createFilterTestForSingleObjectType(
	cmTypeDef configMapTypeDefinition,
	signalT signalType,
	objectT objectType,
	conditionsNamespace1 []string,
	conditionsNamespace2 []string,
) telemetryFilterTestConfig {
	signalsWithoutFiltersDaemonset := allSignals()
	signalsWithoutFiltersDaemonset = slices.DeleteFunc(signalsWithoutFiltersDaemonset, func(s signalType) bool {
		return s == signalT
	})
	signalsWithoutFiltersDeployment := allSignals()
	if signalT == signalTypeMetrics {
		signalsWithoutFiltersDeployment = slices.DeleteFunc(signalsWithoutFiltersDeployment, func(s signalType) bool {
			return s == signalT
		})
	}

	var telemetryFilter1 dash0v1alpha1.TelemetryFilter
	var telemetryFilter2 dash0v1alpha1.TelemetryFilter
	switch signalT {
	case signalTypeTraces:
		switch objectT {
		case objectTypeSpan:
			telemetryFilter1 = dash0v1alpha1.TelemetryFilter{
				Traces: &dash0v1alpha1.TraceFilter{
					SpanFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace1,
					},
				},
			}
			telemetryFilter2 = dash0v1alpha1.TelemetryFilter{
				Traces: &dash0v1alpha1.TraceFilter{
					SpanFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace2,
					},
				},
			}
		case objectTypeSpanEvent:
			telemetryFilter1 = dash0v1alpha1.TelemetryFilter{
				Traces: &dash0v1alpha1.TraceFilter{
					SpanEventFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace1,
					},
				},
			}
			telemetryFilter2 = dash0v1alpha1.TelemetryFilter{
				Traces: &dash0v1alpha1.TraceFilter{
					SpanEventFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace2,
					},
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeMetrics:
		switch objectT {
		case objectTypeMetric:
			telemetryFilter1 = dash0v1alpha1.TelemetryFilter{
				Metrics: &dash0v1alpha1.MetricFilter{
					MetricFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace1,
					},
				},
			}
			telemetryFilter2 = dash0v1alpha1.TelemetryFilter{
				Metrics: &dash0v1alpha1.MetricFilter{
					MetricFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace2,
					},
				},
			}
		case objectTypeDataPoint:
			telemetryFilter1 = dash0v1alpha1.TelemetryFilter{
				Metrics: &dash0v1alpha1.MetricFilter{
					DataPointFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace1,
					},
				},
			}
			telemetryFilter2 = dash0v1alpha1.TelemetryFilter{
				Metrics: &dash0v1alpha1.MetricFilter{
					DataPointFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace2,
					},
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeLogs:
		switch objectT {
		case objectTypeLogRecord:
			telemetryFilter1 = dash0v1alpha1.TelemetryFilter{
				Logs: &dash0v1alpha1.LogFilter{
					LogRecordFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace1,
					},
				},
			}
			telemetryFilter2 = dash0v1alpha1.TelemetryFilter{
				Logs: &dash0v1alpha1.LogFilter{
					LogRecordFilter: dash0v1alpha1.ObjectTypeFilter{
						Conditions: conditionsNamespace2,
					},
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}
	}

	daemonSetExpectations := filterExpectations{
		signalsWithFilters: []signalType{signalT},
		conditions: conditionExpectationsPerNamespaceAndObjectType{
			namespace1: {
				objectT: conditionsNamespace1,
			},
			namespace2: {
				objectT: conditionsNamespace2,
			},
		},
		signalsWithoutFilters: signalsWithoutFiltersDaemonset,
	}
	var deploymentExpectations filterExpectations
	if signalT == signalTypeMetrics {
		deploymentExpectations = filterExpectations{
			signalsWithFilters: []signalType{signalT},
			conditions: conditionExpectationsPerNamespaceAndObjectType{
				namespace1: {
					objectT: conditionsNamespace1,
				},
				namespace2: {
					objectT: conditionsNamespace2,
				},
			},
			signalsWithoutFilters: signalsWithoutFiltersDaemonset,
		}
	} else {
		deploymentExpectations = filterExpectations{
			signalsWithoutFilters: signalsWithoutFiltersDeployment,
		}
	}

	return telemetryFilterTestConfig{
		configMapTypeDefinition: cmTypeDef,
		telemetryFilters: []NamespacedTelemetryFilter{
			{
				Namespace:       namespace1,
				TelemetryFilter: telemetryFilter1,
			},
			{
				Namespace:       namespace2,
				TelemetryFilter: telemetryFilter2,
			},
		},
		expectations: telemetryFilterTestConfigExpectations{
			namespaces: monitoredNamespaces,
			daemonset:  daemonSetExpectations,
			deployment: deploymentExpectations,
		},
	}
}

func emptyFilterExpectations() filterExpectations {
	return filterExpectations{
		signalsWithoutFilters: allSignals(),
	}
}

func allSignals() []signalType {
	return []signalType{signalTypeTraces, signalTypeMetrics, signalTypeLogs}
}
