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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
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
	assembleConfigMapFunction func(*oTelColConfig, []string, []NamespacedFilter, []NamespacedTransform, bool) (*corev1.ConfigMap, error)
	exporterPipelineNames     []string
}

type conditionExpectationsPerObjectType map[signalType]map[objectType][]string

type filterExpectations struct {
	signalsWithFilters    []signalType
	conditions            conditionExpectationsPerObjectType
	signalsWithoutFilters []signalType
}

type filterTestConfigExpectations struct {
	daemonset  filterExpectations
	deployment filterExpectations
}

type filterTestConfig struct {
	configMapTypeDefinition
	filters      []NamespacedFilter
	expectations filterTestConfigExpectations
}

type groupExpectations struct {
	conditions []string
	statements []string
}

type groupExpectationsPerSignalType map[signalType][]groupExpectations

type transformExpectations struct {
	signalsWithTransforms    []signalType
	groups                   groupExpectationsPerSignalType
	signalsWithoutTransforms []signalType
}

type transformTestConfigExpectations struct {
	daemonset  transformExpectations
	deployment transformExpectations
}

type transformTestConfig struct {
	configMapTypeDefinition
	transforms   []NamespacedTransform
	expectations transformTestConfigExpectations
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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            dash0v1alpha1.Export{},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).To(HaveOccurred())
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render the Dash0 exporter when no endpoint is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render the Dash0 exporter without other exporters, with default settings", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Dataset:  "custom-dataset",
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: "HTTP://endpoint.dash0.com:1234",
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
				DevelopmentMode:   true,
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace:      OperatorNamespace,
				NamePrefix:             namePrefix,
				Export:                 *Dash0ExportWithEndpointAndToken(),
				DevelopmentMode:        false,
				DebugVerbosityDetailed: true,
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Grpc: &dash0v1alpha1.GrpcConfiguration{
						Headers: []dash0v1alpha1.Header{{
							Name:  "Key1",
							Value: "Value1",
						}},
					},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render an arbitrary gRPC exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Grpc: &dash0v1alpha1.GrpcConfiguration{
						Endpoint: "http://example.com:1234",
					},
				},
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Http: &dash0v1alpha1.HttpConfiguration{
						Headers: []dash0v1alpha1.Header{{
							Name:  "Key1",
							Value: "Value1",
						}},
						Encoding: dash0v1alpha1.Proto,
					},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")))
		}, daemonSetAndDeployment)

		DescribeTable("should fail to render an HTTP exporter when no encoding is provided", func(cmTypeDef configMapTypeDefinition) {
			_, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export: dash0v1alpha1.Export{
					Http: &dash0v1alpha1.HttpConfiguration{
						Endpoint: HttpEndpointTest,
						Headers: []dash0v1alpha1.Header{{
							Name:  "Key1",
							Value: "Value1",
						}},
					},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).To(
				MatchError(
					ContainSubstring(
						"no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")))

		}, daemonSetAndDeployment)

		DescribeTable("should render an arbitrary HTTP exporter", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)

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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)
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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)
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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)
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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
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
			}, monitoredNamespaces, nil, nil, false)
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
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			SendBatchMaxSize:  nil,
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		batchProcessorRaw := readFromMap(collectorConfig, []string{"processors", "batch"})
		Expect(batchProcessorRaw).ToNot(BeNil())
		batchProcessor := batchProcessorRaw.(map[string]interface{})
		Expect(batchProcessor).To(HaveLen(0))
	}, daemonSetAndDeployment)

	DescribeTable("should not set send_batch_max_size on batch processor if requested", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			SendBatchMaxSize:  ptr.To(uint32(16384)),
		}, monitoredNamespaces, nil, nil, false)

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
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := readFromMap(collectorConfig, []string{"processors", "resource/clustername"})
		Expect(resourceProcessor).To(BeNil())
		verifyProcessorDoesNotAppearInAnyPipeline(collectorConfig, "resource/clustername")
		selfMonitoringTelemetryResourceRaw := readFromMap(
			collectorConfig,
			[]string{
				"service",
				"telemetry",
				"resource",
			})
		Expect(selfMonitoringTelemetryResourceRaw).ToNot(BeNil())
		selfMonitoringTelemetryResource, ok := selfMonitoringTelemetryResourceRaw.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(selfMonitoringTelemetryResource["k8s.cluster.name"]).To(BeNil())
	}, daemonSetAndDeployment)

	DescribeTable("should render resource processor with k8s.cluster.name if available", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			ClusterName:       "cluster-name",
		}, monitoredNamespaces, nil, nil, false)

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
		metricsProcessors := readPipelineProcessors(pipelines, "metrics/downstream")
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
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
				KubernetesInfrastructureMetricsCollectionEnabled: false,
				KubeletStatsReceiverConfig:                       KubeletStatsReceiverConfig{Enabled: false},
				UseHostMetricsReceiver:                           false,
			}, nil, nil, nil, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiver := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiver).To(BeNil())
			hostmetricsReceiver := readFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
			Expect(hostmetricsReceiver).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).ToNot(ContainElement("kubeletstats"))
			Expect(metricsReceivers).ToNot(ContainElement("hostmetrics"))
		})

		type kubeletStatsReceiverConfigTestWanted struct {
			endpoint           string
			authType           string
			insecureSkipVerify bool
		}

		type kubeletStatsReceiverConfigTest struct {
			kubeletStatsReceiverConfig KubeletStatsReceiverConfig
			wanted                     kubeletStatsReceiverConfigTestWanted
		}

		DescribeTable("should render the kubeletstats and hostmetrics receiver if kubernetes infrastructure metrics collection is enabled",
			func(testConfig kubeletStatsReceiverConfigTest) {
				configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
					OperatorNamespace: OperatorNamespace,
					NamePrefix:        namePrefix,
					Export:            *Dash0ExportWithEndpointAndToken(),
					KubernetesInfrastructureMetricsCollectionEnabled: true,
					KubeletStatsReceiverConfig:                       testConfig.kubeletStatsReceiverConfig,
					UseHostMetricsReceiver:                           true,
				}, nil, nil, nil, nil, nil, false)
				Expect(err).ToNot(HaveOccurred())
				collectorConfig := parseConfigMapContent(configMap)
				kubeletstatsReceiverRaw := readFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
				Expect(kubeletstatsReceiverRaw).ToNot(BeNil())
				kubeletstatsReceiver := kubeletstatsReceiverRaw.(map[string]interface{})
				endpoint := kubeletstatsReceiver["endpoint"]
				Expect(endpoint).To(Equal(testConfig.wanted.endpoint))
				authType := kubeletstatsReceiver["auth_type"]
				Expect(authType).To(Equal(testConfig.wanted.authType))
				insecureSkipVerifyPropertyValue, hasInsecureSkipVerifyProperty := kubeletstatsReceiver["insecure_skip_verify"]
				if testConfig.wanted.insecureSkipVerify {
					Expect(hasInsecureSkipVerifyProperty).To(BeTrue())
					Expect(insecureSkipVerifyPropertyValue).To(BeTrue())
				} else {
					Expect(hasInsecureSkipVerifyProperty).To(BeFalse())
				}

				hostmetricsReceiver := readFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
				Expect(hostmetricsReceiver).ToNot(BeNil())

				pipelines := readPipelines(collectorConfig)
				metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
				Expect(metricsReceivers).ToNot(BeNil())
				Expect(metricsReceivers).To(ContainElement("kubeletstats"))
				Expect(metricsReceivers).To(ContainElement("hostmetrics"))
			},

			Entry("should use node name as endpoint", kubeletStatsReceiverConfigTest{
				kubeletStatsReceiverConfig: KubeletStatsReceiverConfig{
					Enabled:  true,
					Endpoint: "https://${env:K8S_NODE_NAME}:10250",
					AuthType: "serviceAccount",
				},
				wanted: kubeletStatsReceiverConfigTestWanted{
					endpoint: "https://${env:K8S_NODE_NAME}:10250",
					authType: "serviceAccount",
				},
			}),
			Entry("should use node IP as endpoint", kubeletStatsReceiverConfigTest{
				kubeletStatsReceiverConfig: KubeletStatsReceiverConfig{
					Enabled:  true,
					Endpoint: "https://${env:K8S_NODE_IP}:10250",
					AuthType: "serviceAccount",
				},
				wanted: kubeletStatsReceiverConfigTestWanted{
					endpoint: "https://${env:K8S_NODE_IP}:10250",
					authType: "serviceAccount",
				},
			}),
			Entry("should use read-only endpoint", kubeletStatsReceiverConfigTest{
				kubeletStatsReceiverConfig: KubeletStatsReceiverConfig{
					Enabled:  true,
					Endpoint: "http://${env:K8S_NODE_IP}:10255",
					AuthType: "none",
				},
				wanted: kubeletStatsReceiverConfigTestWanted{
					endpoint: "http://${env:K8S_NODE_IP}:10255",
					authType: "none",
				},
			}),
			Entry("should use node IP as endpoint with insecure_skip_verify", kubeletStatsReceiverConfigTest{
				kubeletStatsReceiverConfig: KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "https://${env:K8S_NODE_IP}:10250",
					AuthType:           "serviceAccount",
					InsecureSkipVerify: true,
				},
				wanted: kubeletStatsReceiverConfigTestWanted{
					endpoint:           "https://${env:K8S_NODE_IP}:10250",
					authType:           "serviceAccount",
					insecureSkipVerify: true,
				},
			}),
		)
	})

	Describe("should enable/disable collecting labels and annotations", func() {
		DescribeTable("should not render the label/annotation collection snippet if disabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:                     OperatorNamespace,
				NamePrefix:                            namePrefix,
				Export:                                *Dash0ExportWithEndpointAndToken(),
				CollectPodLabelsAndAnnotationsEnabled: false,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := readFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := readFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).To(BeNil())
			annotationsSnippet := readFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should collect labels and annotation if enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:                     OperatorNamespace,
				NamePrefix:                            namePrefix,
				Export:                                *Dash0ExportWithEndpointAndToken(),
				CollectPodLabelsAndAnnotationsEnabled: true,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := readFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := readFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).ToNot(BeNil())
			annotationsSnippet := readFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).ToNot(BeNil())
		}, daemonSetAndDeployment)
	})

	Describe("discard metrics from unmonitored namespaces", func() {

		type ottlFilterExpressionTestConfig struct {
			monitoredNamespaces []string
			expectedExpression  string
		}

		DescribeTable("should render the namespace filter ottl expression", func(testConfig ottlFilterExpressionTestConfig) {
			expression := renderOttlNamespaceFilter(testConfig.monitoredNamespaces)
			Expect(expression).To(Equal(testConfig.expectedExpression))
		},
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
		)

		DescribeTable("should render the namespace filter and add it to the metrics pipeline", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			filterProcessor := readFromMap(collectorConfig, []string{"processors", "filter/metrics/only_monitored_namespaces"})
			Expect(filterProcessor).ToNot(BeNil())
			filters := readFromMap(filterProcessor, []string{"metrics", "metric"})
			Expect(filters).To(HaveLen(1))
			filterString := filters.([]interface{})[0].(string)
			Expect(filterString).To(Equal(`resource.attributes["k8s.namespace.name"] != nil and resource.attributes["k8s.namespace.name"] != "namespace-1" and resource.attributes["k8s.namespace.name"] != "namespace-2"`))
			pipelines := readPipelines(collectorConfig)
			metricsProcessors := readPipelineProcessors(pipelines, "metrics/downstream")
			Expect(metricsProcessors).ToNot(BeNil())
			Expect(metricsProcessors).To(ContainElement("filter/metrics/only_monitored_namespaces"))
		}, daemonSetAndDeployment)
	})

	Describe("prometheus scraping config", func() {
		var config = &oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
		}

		It("should not render the prometheus scraping config if no namespace has scraping enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, []string{}, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "prometheus"})).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).ToNot(ContainElement("prometheus"))
		})

		It("should render the prometheus scraping config with all namespaces for which scraping is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace-1", "namespace-2", "namespace-3"},
				nil,
				[]string{"namespace-1", "namespace-2"},
				nil,
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

			downstreamMetricsReceivers := readPipelineReceivers(pipelines, "metrics/downstream")
			Expect(downstreamMetricsReceivers).ToNot(BeNil())
			Expect(downstreamMetricsReceivers).To(ContainElement("forward/metrics/prometheus"))
		})
	})

	Describe("log collection", func() {
		var config = &oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
		}

		It("should not render the filelog receiver if no namespace has log collection enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, []string{}, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readFromMap(collectorConfig, []string{"receivers", "filelog"})).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			Expect(pipelines["logs/filelog"]).To(BeNil())
		})

		It("should render the filelog config with all namespaces for which log collection is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace-1", "namespace-2", "namespace-3"},
				[]string{"namespace-1", "namespace-2"},
				nil,
				nil,
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			fileLogReceiverRaw := readFromMap(collectorConfig, []string{"receivers", "filelog"})
			Expect(fileLogReceiverRaw).ToNot(BeNil())
			fileLogReceiver := fileLogReceiverRaw.(map[string]interface{})
			filePatterns := fileLogReceiver["include"].([]interface{})
			Expect(filePatterns).To(HaveLen(2))
			Expect(filePatterns).To(ContainElement("/var/log/pods/namespace-1_*/*/*.log"))
			Expect(filePatterns).To(ContainElement("/var/log/pods/namespace-2_*/*/*.log"))

			pipelines := readPipelines(collectorConfig)
			logsFilelogReceivers := readPipelineReceivers(pipelines, "logs/filelog")
			Expect(logsFilelogReceivers).ToNot(BeNil())
			Expect(logsFilelogReceivers).To(ContainElement("filelog"))
			logsFilelogProcessors := readPipelineProcessors(pipelines, "logs/filelog")
			Expect(logsFilelogProcessors).ToNot(BeNil())
			logsFilelogExporters := readPipelineExporters(pipelines, "logs/filelog")
			Expect(logsFilelogExporters).ToNot(BeNil())
			Expect(logsFilelogExporters).To(ContainElement("forward/logs"))

			downstreamLogsReceivers := readPipelineReceivers(pipelines, "logs/downstream")
			Expect(downstreamLogsReceivers).ToNot(BeNil())
			Expect(downstreamLogsReceivers).To(ContainElement("forward/logs"))
		})
	})

	Describe("configurable filtering of telemetry per namespace", func() {
		var filterTestConfigs []TableEntry
		for _, cmTypeDef := range configMapTypeDefinitions {
			filterTestConfigs = slices.Concat(filterTestConfigs, []TableEntry{

				Entry(fmt.Sprintf("[config map type: %s]: should render no filters if filter list is nil", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters:                 nil,
						expectations: filterTestConfigExpectations{
							daemonset:  emptyFilterExpectations(),
							deployment: emptyFilterExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should render no filters if filter list is empty", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters:                 []NamespacedFilter{},
						expectations: filterTestConfigExpectations{
							daemonset:  emptyFilterExpectations(),
							deployment: emptyFilterExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should render no filters if no filters are configured", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters: []NamespacedFilter{
							{
								Namespace: namespace1,
								// no filters for this namespace
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: filterTestConfigExpectations{
							daemonset:  emptyFilterExpectations(),
							deployment: emptyFilterExpectations(),
						},
					}),

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

				Entry(fmt.Sprintf("[config map type: %s]: should apply trace filter to only one namespace", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters: []NamespacedFilter{
							{
								Namespace: namespace1,
								Filter: dash0v1alpha1.Filter{
									ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
									Traces: &dash0v1alpha1.TraceFilter{
										SpanFilter:      []string{"span condition 1", "span condition 2"},
										SpanEventFilter: []string{"span event condition 1", "span event condition 2"},
									},
								},
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: filterTestConfigExpectations{
							daemonset: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeTraces},
								signalsWithoutFilters: []signalType{signalTypeMetrics, signalTypeLogs},
								conditions: conditionExpectationsPerObjectType{
									signalTypeTraces: {
										objectTypeSpan: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span condition 2)`,
										},
										objectTypeSpanEvent: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span event condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span event condition 2)`,
										},
									},
								},
							},
							deployment: emptyFilterExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply metric filter to only one namespace", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters: []NamespacedFilter{
							{
								Namespace: namespace1,
								Filter: dash0v1alpha1.Filter{
									ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
									Metrics: &dash0v1alpha1.MetricFilter{
										MetricFilter:    []string{"metric condition 1", "metric condition 2"},
										DataPointFilter: []string{"data point condition 1", "data point condition 2"},
									},
								},
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: filterTestConfigExpectations{
							daemonset: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeMetrics},
								signalsWithoutFilters: []signalType{signalTypeTraces, signalTypeLogs},
								conditions: conditionExpectationsPerObjectType{
									signalTypeMetrics: {
										objectTypeMetric: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 2)`,
										},
										objectTypeDataPoint: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 2)`,
										},
									},
								},
							},
							deployment: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeMetrics},
								signalsWithoutFilters: []signalType{signalTypeTraces, signalTypeLogs},
								conditions: conditionExpectationsPerObjectType{
									signalTypeMetrics: {
										objectTypeMetric: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 2)`,
										},
										objectTypeDataPoint: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 2)`,
										},
									},
								},
							},
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply log filter to only one namespace", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters: []NamespacedFilter{
							{
								Namespace: namespace1,
								Filter: dash0v1alpha1.Filter{
									ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
									Logs: &dash0v1alpha1.LogFilter{
										LogRecordFilter: []string{"log record condition 1", "log record condition 2"},
									},
								},
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: filterTestConfigExpectations{
							daemonset: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeLogs},
								signalsWithoutFilters: []signalType{signalTypeTraces, signalTypeMetrics},
								conditions: conditionExpectationsPerObjectType{
									signalTypeLogs: {
										objectTypeLogRecord: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (log record condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (log record condition 2)`,
										},
									},
								},
							},
							deployment: emptyFilterExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply filters for all signals to only one namespace", cmTypeDef.cmType),
					filterTestConfig{
						configMapTypeDefinition: cmTypeDef,
						filters: []NamespacedFilter{
							{
								Namespace: namespace1,
								Filter: dash0v1alpha1.Filter{
									ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
									Traces: &dash0v1alpha1.TraceFilter{
										SpanFilter:      []string{"span condition 1", "span condition 2"},
										SpanEventFilter: []string{"span event condition 1", "span event condition 2"},
									},
									Metrics: &dash0v1alpha1.MetricFilter{
										MetricFilter:    []string{"metric condition 1", "metric condition 2"},
										DataPointFilter: []string{"data point condition 1", "data point condition 2"},
									},
									Logs: &dash0v1alpha1.LogFilter{
										LogRecordFilter: []string{"log record condition 1", "log record condition 2"},
									},
								},
							},
							{
								Namespace: namespace2,
								// no filters for this namespace
							},
						},
						expectations: filterTestConfigExpectations{
							daemonset: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeTraces, signalTypeMetrics, signalTypeLogs},
								signalsWithoutFilters: nil,
								conditions: conditionExpectationsPerObjectType{
									signalTypeTraces: {
										objectTypeSpan: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span condition 2)`,
										},
										objectTypeSpanEvent: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span event condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (span event condition 2)`,
										},
									},
									signalTypeMetrics: {
										objectTypeMetric: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 2)`,
										},
										objectTypeDataPoint: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 2)`,
										},
									},
									signalTypeLogs: {
										objectTypeLogRecord: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (log record condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (log record condition 2)`,
										},
									},
								},
							},
							deployment: filterExpectations{
								signalsWithFilters:    []signalType{signalTypeMetrics},
								signalsWithoutFilters: []signalType{signalTypeTraces, signalTypeLogs},
								conditions: conditionExpectationsPerObjectType{
									signalTypeMetrics: {
										objectTypeMetric: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (metric condition 2)`,
										},
										objectTypeDataPoint: []string{
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 1)`,
											`resource.attributes["k8s.namespace.name"] == "namespace-1" and (data point condition 2)`,
										},
									},
								},
							},
						},
					}),

				//
			})
		}

		DescribeTable("filter telemetry", func(testConfig filterTestConfig) {
			configMap, err := testConfig.assembleConfigMapFunction(
				&oTelColConfig{
					OperatorNamespace: OperatorNamespace,
					NamePrefix:        namePrefix,
					Export:            *Dash0ExportWithEndpointAndToken(),
				},
				monitoredNamespaces,
				testConfig.filters,
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())

			var expectations filterExpectations
			switch testConfig.cmType {
			case configMapTypeDaemonSet:
				expectations = testConfig.expectations.daemonset
			case configMapTypeDeployment:
				expectations = testConfig.expectations.deployment
			}

			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)
			for _, signal := range expectations.signalsWithFilters {
				filterProcessorName := fmt.Sprintf("filter/%s/custom_telemetry_filter", signal)
				filterProcessorRaw := readFromMap(
					collectorConfig,
					[]string{"processors", filterProcessorName},
				)
				Expect(filterProcessorRaw).ToNot(BeNil(),
					fmt.Sprintf("expected filter processor %s to exist, but it didn't", filterProcessorName))
				filterProcessor := filterProcessorRaw.(map[string]interface{})
				Expect(filterProcessor["error_mode"]).To(Equal("ignore"))

				for objectType, expectedConditionsForObjectType := range expectations.conditions[signal] {
					hasExpectedConditions := false
					if len(expectedConditionsForObjectType) > 0 {
						hasExpectedConditions = true
					}
					filterConditionsRaw := readFromMap(filterProcessor, []string{string(signal), string(objectType)})
					Expect(filterConditionsRaw).ToNot(BeNil(),
						"expected %d filter conditions but there were none for signal \"%s\" and object type \"%s\"",
						len(expectedConditionsForObjectType), signal, objectType)
					actualFilterConditions := filterConditionsRaw.([]interface{})
					Expect(actualFilterConditions).To(HaveLen(len(expectedConditionsForObjectType)))
					for i, expectedCondition := range expectedConditionsForObjectType {
						Expect(actualFilterConditions[i]).To(Equal(expectedCondition))
					}
					if !hasExpectedConditions {
						Fail(
							fmt.Sprintf(
								"expected conditions are empty for signal %s and object type %s, although there should "+
									"be conditions for this combination of signal type and object type; this is "+
									"probably an error in the test config for this test case",
								signal,
								objectType,
							))
					}
				}

				downstreamPipelineProcessors := readPipelineProcessors(pipelines, fmt.Sprintf("%s/downstream", signal))
				Expect(downstreamPipelineProcessors).To(ContainElements(filterProcessorName))
			}

			for _, signal := range expectations.signalsWithoutFilters {
				filterProcessorName := fmt.Sprintf("filter/%s/custom_telemetry_filter", signal)
				filterProcessorRaw := readFromMap(
					collectorConfig,
					[]string{"processors", filterProcessorName},
				)
				Expect(filterProcessorRaw).To(BeNil(),
					fmt.Sprintf("expected filter processor %s to be nil, but it wasn't", filterProcessorName))

				verifyProcessorDoesNotAppearInAnyPipeline(
					collectorConfig,
					filterProcessorName,
				)
			}
		}, filterTestConfigs)

		type filterErrorModeTestConfig struct {
			errorModes []dash0v1alpha1.FilterTransformErrorMode
			expected   dash0v1alpha1.FilterTransformErrorMode
		}

		DescribeTable("filter processor should use the most severe error mode", func(testConfig filterErrorModeTestConfig) {
			var filters []NamespacedFilter
			for _, errorMode := range testConfig.errorModes {
				filters = append(filters, NamespacedFilter{
					Namespace: namespace1,
					Filter: dash0v1alpha1.Filter{
						ErrorMode: errorMode,
						Traces: &dash0v1alpha1.TraceFilter{
							SpanFilter: []string{
								"condition",
							},
						},
					},
				})
			}
			result := aggregateCustomFilters(filters)
			Expect(result.ErrorMode).To(Equal(testConfig.expected))

		},
			Entry("no error mode provided", filterErrorModeTestConfig{
				errorModes: nil,
				expected:   dash0v1alpha1.FilterTransformErrorModeIgnore,
			}),
			Entry("single error mode is used", filterErrorModeTestConfig{
				errorModes: []dash0v1alpha1.FilterTransformErrorMode{dash0v1alpha1.FilterTransformErrorModeSilent},
				expected:   dash0v1alpha1.FilterTransformErrorModeSilent,
			}),
			Entry("most severe error mode is used", filterErrorModeTestConfig{
				errorModes: []dash0v1alpha1.FilterTransformErrorMode{
					dash0v1alpha1.FilterTransformErrorModeSilent,
					dash0v1alpha1.FilterTransformErrorModeIgnore,
					dash0v1alpha1.FilterTransformErrorModePropagate,
				},
				expected: dash0v1alpha1.FilterTransformErrorModePropagate,
			}),
		)
	})

	Describe("configurable transformation of telemetry per namespace", func() {
		var transformTestConfigs []TableEntry
		for _, cmTypeDef := range configMapTypeDefinitions {
			transformTestConfigs = slices.Concat(transformTestConfigs, []TableEntry{

				Entry(fmt.Sprintf("[config map type: %s]: should render no transforms if transform list is nil", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms:              nil,
						expectations: transformTestConfigExpectations{
							daemonset:  emptyTransformExpectations(),
							deployment: emptyTransformExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should render no transforms if transform list is empty", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms:              []NamespacedTransform{},
						expectations: transformTestConfigExpectations{
							daemonset:  emptyTransformExpectations(),
							deployment: emptyTransformExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should render no transforms if no transforms are configured", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms: []NamespacedTransform{
							{
								Namespace: namespace1,
								// no transforms for this namespace
							},
							{
								Namespace: namespace2,
								// no transforms for this namespace
							},
						},
						expectations: transformTestConfigExpectations{
							daemonset:  emptyTransformExpectations(),
							deployment: emptyTransformExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should transform traces", cmTypeDef.cmType),
					createTransformTestForSingleObjectType(cmTypeDef,
						signalTypeTraces,
						[]string{"statement 1", "statement 2"},
						[]string{"statement 3", "statement 4"},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should transform metrics", cmTypeDef.cmType),
					createTransformTestForSingleObjectType(cmTypeDef,
						signalTypeMetrics,
						[]string{"statement 1", "statement 2"},
						[]string{"statement 3", "statement 4"},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should transform logs", cmTypeDef.cmType),
					createTransformTestForSingleObjectType(cmTypeDef,
						signalTypeLogs,
						[]string{"statement 1", "statement 2"},
						[]string{"statement 3", "statement 4"},
					)),

				Entry(fmt.Sprintf("[config map type: %s]: should apply trace transform to only one namespace", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms: []NamespacedTransform{
							{
								Namespace: namespace1,
								Transform: dash0v1alpha1.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
									Traces: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"statement 1"}},
										{Statements: []string{"statement 2"}},
									},
								},
							},
							{
								Namespace: namespace2,
								// no transforms for this namespace
							},
						},
						expectations: transformTestConfigExpectations{
							daemonset: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeTraces},
								signalsWithoutTransforms: []signalType{signalTypeMetrics, signalTypeLogs},
								groups: groupExpectationsPerSignalType{
									signalTypeTraces: []groupExpectations{
										{
											statements: []string{"statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
								},
							},
							deployment: emptyTransformExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply metric transform to only one namespace", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms: []NamespacedTransform{
							{
								Namespace: namespace1,
								Transform: dash0v1alpha1.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
									Metrics: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"statement 1"}},
										{Statements: []string{"statement 2"}},
									},
								},
							},
							{
								Namespace: namespace2,
								// no transforms for this namespace
							},
						},
						expectations: transformTestConfigExpectations{
							daemonset: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeMetrics},
								signalsWithoutTransforms: []signalType{signalTypeTraces, signalTypeLogs},
								groups: groupExpectationsPerSignalType{
									signalTypeMetrics: {
										{
											statements: []string{"statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
								},
							},
							deployment: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeMetrics},
								signalsWithoutTransforms: []signalType{signalTypeTraces, signalTypeLogs},
								groups: groupExpectationsPerSignalType{
									signalTypeMetrics: []groupExpectations{
										{
											statements: []string{"statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
								},
							},
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply log transform to only one namespace", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms: []NamespacedTransform{
							{
								Namespace: namespace2,
								Transform: dash0v1alpha1.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
									Logs: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"statement 1"}},
										{Statements: []string{"statement 2"}},
									},
								},
							},
							{
								Namespace: namespace1,
								// no transforms for this namespace
							},
						},
						expectations: transformTestConfigExpectations{
							daemonset: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeLogs},
								signalsWithoutTransforms: []signalType{signalTypeTraces, signalTypeMetrics},
								groups: groupExpectationsPerSignalType{
									signalTypeLogs: []groupExpectations{
										{
											statements: []string{"statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-2"`,
											},
										},
										{
											statements: []string{"statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-2"`,
											},
										},
									},
								},
							},
							deployment: emptyTransformExpectations(),
						},
					}),

				Entry(fmt.Sprintf("[config map type: %s]: should apply transforms for all signals to only one namespace", cmTypeDef.cmType),
					transformTestConfig{
						configMapTypeDefinition: cmTypeDef,
						transforms: []NamespacedTransform{
							{
								Namespace: namespace1,
								Transform: dash0v1alpha1.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
									Traces: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"trace statement 1"}},
										{Statements: []string{"trace statement 2"}},
									},
									Metrics: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"metric statement 1"}},
										{Statements: []string{"metric statement 2"}},
									},
									Logs: []dash0v1alpha1.NormalizedTransformGroup{
										{Statements: []string{"log statement 1"}},
										{Statements: []string{"log statement 2"}},
									},
								},
							},
							{
								Namespace: namespace2,
								// no transforms for this namespace
							},
						},
						expectations: transformTestConfigExpectations{
							daemonset: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeTraces, signalTypeMetrics, signalTypeLogs},
								signalsWithoutTransforms: nil,
								groups: groupExpectationsPerSignalType{
									signalTypeTraces: []groupExpectations{
										{
											statements: []string{"trace statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"trace statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
									signalTypeMetrics: []groupExpectations{
										{
											statements: []string{"metric statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"metric statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
									signalTypeLogs: []groupExpectations{
										{
											statements: []string{"log statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"log statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
								},
							},
							deployment: transformExpectations{
								signalsWithTransforms:    []signalType{signalTypeMetrics},
								signalsWithoutTransforms: []signalType{signalTypeTraces, signalTypeLogs},
								groups: groupExpectationsPerSignalType{
									signalTypeMetrics: {
										{
											statements: []string{"metric statement 1"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
										{
											statements: []string{"metric statement 2"},
											conditions: []string{
												`resource.attributes["k8s.namespace.name"] == "namespace-1"`,
											},
										},
									},
								},
							},
						},
					}),

				//
			})
		}

		DescribeTable("transform telemetry", func(testConfig transformTestConfig) {
			configMap, err := testConfig.assembleConfigMapFunction(
				&oTelColConfig{
					OperatorNamespace: OperatorNamespace,
					NamePrefix:        namePrefix,
					Export:            *Dash0ExportWithEndpointAndToken(),
				},
				monitoredNamespaces,
				nil,
				testConfig.transforms,
				false,
			)
			Expect(err).ToNot(HaveOccurred())

			var expectations transformExpectations
			switch testConfig.cmType {
			case configMapTypeDaemonSet:
				expectations = testConfig.expectations.daemonset
			case configMapTypeDeployment:
				expectations = testConfig.expectations.deployment
			}

			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)
			for _, signal := range expectations.signalsWithTransforms {
				transformProcessorName := fmt.Sprintf("transform/%s/custom_telemetry_transform", signal)
				transformProcessorRaw := readFromMap(
					collectorConfig,
					[]string{"processors", transformProcessorName},
				)
				Expect(transformProcessorRaw).ToNot(BeNil(),
					fmt.Sprintf("expected transform processor %s to exist, but it didn't", transformProcessorName))
				transformProcessor := transformProcessorRaw.(map[string]interface{})
				Expect(transformProcessor["error_mode"]).To(Equal("ignore"))

				signalGroupsLabel := ""
				switch signal {
				case signalTypeTraces:
					signalGroupsLabel = "trace_statements"
				case signalTypeMetrics:
					signalGroupsLabel = "metric_statements"
				case signalTypeLogs:
					signalGroupsLabel = "log_statements"
				default:
					Fail(fmt.Sprintf("unknown signal type: %s", signal))
				}

				expectedGroups := expectations.groups[signal]
				groupsRaw := readFromMap(transformProcessor, []string{signalGroupsLabel})
				Expect(groupsRaw).ToNot(BeNil(),
					"expected %d transform group(s) but there were none for signal \"%s\"",
					len(expectedGroups), signal)
				groups := groupsRaw.([]interface{})
				Expect(groups).To(HaveLen(len(expectedGroups)))

				for i, expectedGroup := range expectedGroups {
					expectedStatements := expectedGroup.statements
					actualStatementsRaw := readFromMap(groups[i], []string{"statements"})
					Expect(actualStatementsRaw).ToNot(BeNil(),
						"expected %d transform statement(s) but there were none for signal \"%s\"",
						len(expectedStatements), signal)
					actualTransformStatements := actualStatementsRaw.([]interface{})
					Expect(actualTransformStatements).To(HaveLen(len(expectedStatements)))
					for i, expectedStatement := range expectedStatements {
						Expect(actualTransformStatements[i]).To(Equal(expectedStatement))
					}

					expectedConditions := expectedGroup.conditions
					actualConditionsRaw := readFromMap(groups[i], []string{"conditions"})
					Expect(actualConditionsRaw).ToNot(BeNil(),
						"expected %d transform condition(s) but there were none for signal \"%s\"",
						len(expectedConditions), signal)
					actualTransformConditions := actualConditionsRaw.([]interface{})
					Expect(actualTransformConditions).To(HaveLen(len(expectedConditions)))
					for i, expectedCondition := range expectedConditions {
						Expect(actualTransformConditions[i]).To(Equal(expectedCondition))
					}
				}

				downstreamPipelineProcessors := readPipelineProcessors(pipelines, fmt.Sprintf("%s/downstream", signal))
				Expect(downstreamPipelineProcessors).To(ContainElements(transformProcessorName))
			}

			for _, signal := range expectations.signalsWithoutTransforms {
				transformProcessorName := fmt.Sprintf("transform/%s/custom_telemetry_transform", signal)
				transformProcessorRaw := readFromMap(
					collectorConfig,
					[]string{"processors", transformProcessorName},
				)
				Expect(transformProcessorRaw).To(BeNil(),
					fmt.Sprintf("expected transform processor %s to be nil, but it wasn't", transformProcessorName))

				verifyProcessorDoesNotAppearInAnyPipeline(
					collectorConfig,
					transformProcessorName,
				)
			}
		}, transformTestConfigs)

		type transformErrorModeTestConfig struct {
			errorModes []dash0v1alpha1.FilterTransformErrorMode
			expected   dash0v1alpha1.FilterTransformErrorMode
		}

		DescribeTable("transform processor should use the most severe error mode", func(testConfig transformErrorModeTestConfig) {
			var transforms []NamespacedTransform
			for _, errorMode := range testConfig.errorModes {
				transforms = append(transforms, NamespacedTransform{
					Namespace: namespace1,
					Transform: dash0v1alpha1.NormalizedTransformSpec{
						ErrorMode: ptr.To(errorMode),
						Traces: []dash0v1alpha1.NormalizedTransformGroup{
							{Statements: []string{"statement"}},
						},
					},
				})
			}
			result := aggregateCustomTransforms(transforms)
			Expect(result.GlobalErrorMode).To(Equal(testConfig.expected))

		},
			Entry("no error mode provided", transformErrorModeTestConfig{
				errorModes: nil,
				expected:   dash0v1alpha1.FilterTransformErrorModeIgnore,
			}),
			Entry("single error mode is used", transformErrorModeTestConfig{
				errorModes: []dash0v1alpha1.FilterTransformErrorMode{dash0v1alpha1.FilterTransformErrorModeSilent},
				expected:   dash0v1alpha1.FilterTransformErrorModeSilent,
			}),
			Entry("most severe error mode is used", transformErrorModeTestConfig{
				errorModes: []dash0v1alpha1.FilterTransformErrorMode{
					dash0v1alpha1.FilterTransformErrorModeSilent,
					dash0v1alpha1.FilterTransformErrorModeIgnore,
					dash0v1alpha1.FilterTransformErrorModePropagate,
				},
				expected: dash0v1alpha1.FilterTransformErrorModePropagate,
			}),
		)
	})

	Describe("on an IPv4 or IPv6 cluster", func() {
		type ipVersionTestConfig struct {
			ipv6     bool
			expected string
		}

		DescribeTable("should render IPv4 addresses in an IPv4 cluster", func(testConfig *ipVersionTestConfig) {
			var config = &oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
				IsIPv6Cluster:     testConfig.ipv6,
			}

			expected := testConfig.expected
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, nil, nil, nil, false)
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

			configMap, err = assembleDeploymentCollectorConfigMap(config, nil, nil, nil, false)
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
		},
			Entry("IPv4 cluster", &ipVersionTestConfig{
				ipv6:     false,
				expected: "${env:K8S_POD_IP}",
			}),
			Entry("IPv6 cluster", &ipVersionTestConfig{
				ipv6:     true,
				expected: "[${env:K8S_POD_IP}]",
			}),
		)
	})

	Describe("render logs self monitoring pipeline", func() {

		DescribeTable("should not render a log pipeline if self monitoring is not enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
				SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
					SelfMonitoringEnabled: false,
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			selfMonitoringLogsPipeline := readSelfMonitoringLogsPipeline(collectorConfig)
			Expect(selfMonitoringLogsPipeline).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should not render a log pipeline if there is no self monitoring export", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            *Dash0ExportWithEndpointAndToken(),
				SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
					SelfMonitoringEnabled: true,
					Export:                dash0v1alpha1.Export{},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			selfMonitoringLogsPipeline := readSelfMonitoringLogsPipeline(collectorConfig)
			Expect(selfMonitoringLogsPipeline).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should render a log pipeline for a Dash0 export", func(cmTypeDef configMapTypeDefinition) {
			export := *Dash0ExportWithEndpointAndToken()
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Export:            export,
				SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
					SelfMonitoringEnabled: true,
					Export:                export,
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			selfMonitoringLogsPipelineRaw := readSelfMonitoringLogsPipeline(collectorConfig)
			Expect(selfMonitoringLogsPipelineRaw).ToNot(BeNil())
			selfMonitoringLogsPipeline, ok := selfMonitoringLogsPipelineRaw.(map[string]interface{})
			Expect(ok).To(BeTrue())
			otlpExporter := readOtlpExporterFromSelfMonitoringLogsPipeline(selfMonitoringLogsPipeline)
			Expect(otlpExporter["protocol"]).To(Equal(common.ProtocolGrpc))
			Expect(otlpExporter["endpoint"]).To(Equal(EndpointDash0WithProtocolTest))
			headersRaw := otlpExporter["headers"]
			Expect(headersRaw).ToNot(BeNil())
			headers, ok := headersRaw.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(headers).To(HaveLen(1))
			Expect(headers[util.AuthorizationHeaderName]).To(Equal("Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"))
		}, daemonSetAndDeployment)
	})

	DescribeTable("should render a log pipeline for a gRPC export", func(cmTypeDef configMapTypeDefinition) {
		export := *GrpcExportTest()
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                export,
			},
		}, monitoredNamespaces, nil, nil, false)
		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		selfMonitoringLogsPipelineRaw := readSelfMonitoringLogsPipeline(collectorConfig)
		Expect(selfMonitoringLogsPipelineRaw).ToNot(BeNil())
		selfMonitoringLogsPipeline, ok := selfMonitoringLogsPipelineRaw.(map[string]interface{})
		Expect(ok).To(BeTrue())
		otlpExporter := readOtlpExporterFromSelfMonitoringLogsPipeline(selfMonitoringLogsPipeline)
		Expect(otlpExporter["protocol"]).To(Equal(common.ProtocolGrpc))
		Expect(otlpExporter["endpoint"]).To(Equal(EndpointGrpcWithProtocolTest))
		headersRaw := otlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers, ok := headersRaw.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key"]).To(Equal("Value"))
	}, daemonSetAndDeployment)

	DescribeTable("should render a log pipeline for an HTTP export", func(cmTypeDef configMapTypeDefinition) {
		export := *HttpExportTest()
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                export,
			},
		}, monitoredNamespaces, nil, nil, false)
		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		selfMonitoringLogsPipelineRaw := readSelfMonitoringLogsPipeline(collectorConfig)
		Expect(selfMonitoringLogsPipelineRaw).ToNot(BeNil())
		selfMonitoringLogsPipeline, ok := selfMonitoringLogsPipelineRaw.(map[string]interface{})
		Expect(ok).To(BeTrue())
		otlpExporter := readOtlpExporterFromSelfMonitoringLogsPipeline(selfMonitoringLogsPipeline)
		Expect(otlpExporter["protocol"]).To(Equal(common.ProtocolHttpProtobuf))
		Expect(otlpExporter["endpoint"]).To(Equal(EndpointHttpTest))
		headersRaw := otlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers, ok := headersRaw.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key"]).To(Equal("Value"))
	}, daemonSetAndDeployment)
})

func assembleDaemonSetCollectorConfigMapWithoutScrapingNamespaces(
	config *oTelColConfig,
	monitoredNamespaces []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDaemonSetCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil,
		nil,
		filters,
		transforms,
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMapForTest(
	config *oTelColConfig,
	monitoredNamespaces []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleDeploymentCollectorConfigMap(
		config,
		monitoredNamespaces,
		filters,
		transforms,
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
	processorName string,
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

func readSelfMonitoringLogsPipeline(collectorConfig map[string]interface{}) interface{} {
	return readFromMap(
		collectorConfig,
		[]string{
			"service",
			"telemetry",
			"logs",
		})
}

func readOtlpExporterFromSelfMonitoringLogsPipeline(selfMonitoringLogsPipeline map[string]interface{}) map[string]interface{} {
	otlpExporterRaw := readFromMap(
		selfMonitoringLogsPipeline,
		[]string{
			"processors",
			"0",
			"batch",
			"exporter",
			"otlp",
		},
	)
	Expect(otlpExporterRaw).ToNot(BeNil())
	otlpExporter, ok := otlpExporterRaw.(map[string]interface{})
	Expect(ok).To(BeTrue())
	return otlpExporter
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
) filterTestConfig {
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

	var filter1 dash0v1alpha1.Filter
	var filter2 dash0v1alpha1.Filter
	switch signalT {
	case signalTypeTraces:
		switch objectT {
		case objectTypeSpan:
			filter1 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Traces: &dash0v1alpha1.TraceFilter{
					SpanFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Traces: &dash0v1alpha1.TraceFilter{
					SpanFilter: conditionsNamespace2,
				},
			}
		case objectTypeSpanEvent:
			filter1 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Traces: &dash0v1alpha1.TraceFilter{
					SpanEventFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Traces: &dash0v1alpha1.TraceFilter{
					SpanEventFilter: conditionsNamespace2,
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeMetrics:
		switch objectT {
		case objectTypeMetric:
			filter1 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Metrics: &dash0v1alpha1.MetricFilter{
					MetricFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Metrics: &dash0v1alpha1.MetricFilter{
					MetricFilter: conditionsNamespace2,
				},
			}
		case objectTypeDataPoint:
			filter1 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Metrics: &dash0v1alpha1.MetricFilter{
					DataPointFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Metrics: &dash0v1alpha1.MetricFilter{
					DataPointFilter: conditionsNamespace2,
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeLogs:
		switch objectT {
		case objectTypeLogRecord:
			filter1 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Logs: &dash0v1alpha1.LogFilter{
					LogRecordFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0v1alpha1.Filter{
				ErrorMode: dash0v1alpha1.FilterTransformErrorModeIgnore,
				Logs: &dash0v1alpha1.LogFilter{
					LogRecordFilter: conditionsNamespace2,
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}
	}

	daemonSetExpectations := filterExpectations{
		signalsWithFilters: []signalType{signalT},
		conditions: conditionExpectationsPerObjectType{
			signalT: {
				objectT: slices.Concat(
					prependNamespaceCheckToAllOttlCondition(namespace1, conditionsNamespace1),
					prependNamespaceCheckToAllOttlCondition(namespace2, conditionsNamespace2),
				),
			},
		},
		signalsWithoutFilters: signalsWithoutFiltersDaemonset,
	}
	var deploymentExpectations filterExpectations
	if signalT == signalTypeMetrics {
		deploymentExpectations = filterExpectations{
			signalsWithFilters: []signalType{signalT},
			conditions: conditionExpectationsPerObjectType{
				signalT: {
					objectT: slices.Concat(
						prependNamespaceCheckToAllOttlCondition(namespace1, conditionsNamespace1),
						prependNamespaceCheckToAllOttlCondition(namespace2, conditionsNamespace2),
					),
				},
			},
			signalsWithoutFilters: signalsWithoutFiltersDaemonset,
		}
	} else {
		deploymentExpectations = filterExpectations{
			signalsWithoutFilters: signalsWithoutFiltersDeployment,
		}
	}

	return filterTestConfig{
		configMapTypeDefinition: cmTypeDef,
		filters: []NamespacedFilter{
			{
				Namespace: namespace1,
				Filter:    filter1,
			},
			{
				Namespace: namespace2,
				Filter:    filter2,
			},
		},
		expectations: filterTestConfigExpectations{
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

func prependNamespaceCheckToAllOttlCondition(namespace string, conditions []string) []string {
	processedConditions := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		processedConditions = append(
			processedConditions,
			prependNamespaceCheckToOttlFilterCondition(namespace, condition),
		)
	}
	return processedConditions
}

func createTransformTestForSingleObjectType(
	cmTypeDef configMapTypeDefinition,
	signalT signalType,
	statementsNamespace1 []string,
	statementsNamespace2 []string,
) transformTestConfig {
	signalsWithoutTransformsDaemonset := allSignals()
	signalsWithoutTransformsDaemonset = slices.DeleteFunc(signalsWithoutTransformsDaemonset, func(s signalType) bool {
		return s == signalT
	})
	signalsWithoutTransformsDeployment := allSignals()
	if signalT == signalTypeMetrics {
		signalsWithoutTransformsDeployment = slices.DeleteFunc(signalsWithoutTransformsDeployment, func(s signalType) bool {
			return s == signalT
		})
	}

	var transform1 dash0v1alpha1.NormalizedTransformSpec
	var transform2 dash0v1alpha1.NormalizedTransformSpec
	switch signalT {
	case signalTypeTraces:
		transform1 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Traces: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Traces: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace2},
			},
		}

	case signalTypeMetrics:
		transform1 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Metrics: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Metrics: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace2},
			},
		}

	case signalTypeLogs:
		transform1 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Logs: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0v1alpha1.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0v1alpha1.FilterTransformErrorModeIgnore),
			Logs: []dash0v1alpha1.NormalizedTransformGroup{
				{Statements: statementsNamespace2},
			},
		}
	}

	daemonSetExpectations := transformExpectations{
		signalsWithTransforms: []signalType{signalT},
		groups: groupExpectationsPerSignalType{
			signalT: []groupExpectations{
				{
					conditions: []string{
						fmt.Sprintf(
							`resource.attributes["k8s.namespace.name"] == "%s"`,
							namespace1,
						)},
					statements: statementsNamespace1,
				},
				{
					conditions: []string{
						fmt.Sprintf(
							`resource.attributes["k8s.namespace.name"] == "%s"`,
							namespace2,
						)},
					statements: statementsNamespace2,
				},
			},
		},
		signalsWithoutTransforms: signalsWithoutTransformsDaemonset,
	}
	var deploymentExpectations transformExpectations
	if signalT == signalTypeMetrics {
		deploymentExpectations = transformExpectations{
			signalsWithTransforms: []signalType{signalT},
			groups: groupExpectationsPerSignalType{
				signalT: []groupExpectations{
					{
						conditions: []string{
							fmt.Sprintf(
								`resource.attributes["k8s.namespace.name"] == "%s"`,
								namespace1,
							)},
						statements: statementsNamespace1,
					},
					{
						conditions: []string{
							fmt.Sprintf(
								`resource.attributes["k8s.namespace.name"] == "%s"`,
								namespace2,
							)},
						statements: statementsNamespace2,
					},
				},
			},
			signalsWithoutTransforms: signalsWithoutTransformsDaemonset,
		}
	} else {
		deploymentExpectations = transformExpectations{
			signalsWithoutTransforms: signalsWithoutTransformsDeployment,
		}
	}

	return transformTestConfig{
		configMapTypeDefinition: cmTypeDef,
		transforms: []NamespacedTransform{
			{
				Namespace: namespace1,
				Transform: transform1,
			},
			{
				Namespace: namespace2,
				Transform: transform2,
			},
		},
		expectations: transformTestConfigExpectations{
			daemonset:  daemonSetExpectations,
			deployment: deploymentExpectations,
		},
	}
}

func emptyTransformExpectations() transformExpectations {
	return transformExpectations{
		signalsWithoutTransforms: allSignals(),
	}
}
