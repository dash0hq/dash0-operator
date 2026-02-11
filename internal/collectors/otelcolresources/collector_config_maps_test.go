// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
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
	grpcEndpointTest         = "example.com:4317"
	httpInsecureEndpointTest = "http://example.com:1234"
	namespace1               = "namespace-1"
	namespace2               = "namespace-2"
	namespace3               = "namespace-3"
)

var (
	bearerWithAuthToken                  = fmt.Sprintf("Bearer ${env:%s}", authEnvVarNameDefault)
	monitoredNamespaces                  = []string{namespace1, namespace2}
	emptyTargetAllocatorMtlsConfig       = TargetAllocatorMtlsConfig{}
	defaultNamespacesWithEventCollection = []string{namespace2, namespace3}
)

func cmTestSingleDefaultOtlpExporter() otlpExporters {
	exporter, _ := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	return otlpExporters{
		Default:    []otlpExporter{*exporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestSingleDefaultOtlpExporterWithCustomDs() otlpExporters {
	exporter, _ := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointTokenAndCustomDataset().Dash0, "default", authEnvVarNameDefault)
	return otlpExporters{
		Default:    []otlpExporter{*exporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestDash0AndGrpcExporters() otlpExporters {
	dash0Exporter, err := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	Expect(err).ToNot(HaveOccurred())
	grpcExporter, err := convertGrpcExporterToOtlpExporter(GrpcExportTest().Grpc, "default")
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default:    []otlpExporter{*dash0Exporter, *grpcExporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestDash0AndHttpExporters() otlpExporters {
	dash0Exporter, err := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	Expect(err).ToNot(HaveOccurred())
	httpExporter, err := convertHttpExporterToOtlpExporter(HttpExportTest().Http, "default")
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default:    []otlpExporter{*dash0Exporter, *httpExporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestDash0GrpcAndHttpExporters() otlpExporters {
	dash0Exporter, err := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	Expect(err).ToNot(HaveOccurred())
	grpcExporter, err := convertGrpcExporterToOtlpExporter(GrpcExportTest().Grpc, "default")
	Expect(err).ToNot(HaveOccurred())
	httpExporter, err := convertHttpExporterToOtlpExporter(HttpExportTest().Http, "default")
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default:    []otlpExporter{*dash0Exporter, *grpcExporter, *httpExporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestGrpcExporterWithInsecure() otlpExporters {
	grpcExporter, err := convertGrpcExporterToOtlpExporter(&dash0common.GrpcConfiguration{
		Endpoint: grpcEndpointTest,
		Insecure: ptr.To(true),
		Headers: []dash0common.Header{{
			Name:  "Key",
			Value: "Value",
		}},
	}, "default")
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default:    []otlpExporter{*grpcExporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestHttpExporterWithInsecure() otlpExporters {
	httpExporter, err := convertHttpExporterToOtlpExporter(&dash0common.HttpConfiguration{
		Endpoint: httpInsecureEndpointTest,
		Encoding: dash0common.Proto,
		Headers: []dash0common.Header{{
			Name:  "Key",
			Value: "Value",
		}},
	}, "default")
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default:    []otlpExporter{*httpExporter},
		Namespaced: make(map[string][]otlpExporter),
	}
}

func cmTestNamespacedOtlpExporters() otlpExporters {
	dash0ExporterDefault, err := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	Expect(err).ToNot(HaveOccurred())
	grpcExporterNs1, err := convertGrpcExporterToOtlpExporter(GrpcExportTest().Grpc, "ns/"+namespace1)
	Expect(err).ToNot(HaveOccurred())
	httpExporterNs2, err := convertHttpExporterToOtlpExporter(HttpExportTest().Http, "ns/"+namespace2)
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default: []otlpExporter{*dash0ExporterDefault},
		Namespaced: map[string][]otlpExporter{
			namespace1: {*grpcExporterNs1},
			namespace2: {*httpExporterNs2},
		},
	}
}

func cmTestMultipleNamespacedOtlpExporters() otlpExporters {
	dash0ExporterDefault, err := convertDash0ExporterToOtlpExporter(Dash0ExportWithEndpointAndToken().Dash0, "default", authEnvVarNameDefault)
	Expect(err).ToNot(HaveOccurred())
	grpcExporterNs1, err := convertGrpcExporterToOtlpExporter(GrpcExportTest().Grpc, "ns/"+namespace1)
	Expect(err).ToNot(HaveOccurred())
	httpExporterNs1, err := convertHttpExporterToOtlpExporter(HttpExportTest().Http, "ns/"+namespace1)
	Expect(err).ToNot(HaveOccurred())
	dash0ExporterNs2, err := convertDash0ExporterToOtlpExporter(&dash0common.Dash0Configuration{
		Endpoint: EndpointDash0TestAlternative,
		Authorization: dash0common.Authorization{
			Token: &AuthorizationTokenTestAlternative,
		},
	}, "ns/"+namespace2, authEnvVarNameForNs(namespace2))
	Expect(err).ToNot(HaveOccurred())
	return otlpExporters{
		Default: []otlpExporter{*dash0ExporterDefault},
		Namespaced: map[string][]otlpExporter{
			namespace1: {*grpcExporterNs1, *httpExporterNs1},
			namespace2: {*dash0ExporterNs2},
		},
	}
}

var _ = Describe("The OpenTelemetry Collector ConfigMaps", func() {

	configMapTypeDefinitions := []configMapTypeDefinition{
		{
			cmType:                    configMapTypeDaemonSet,
			assembleConfigMapFunction: assembleDaemonSetCollectorConfigMapForTest,
		},
		{
			cmType:                    configMapTypeDeployment,
			assembleConfigMapFunction: assembleDeploymentCollectorConfigMapForTest,
		},
	}

	daemonSetAndDeployment := make([]TableEntry, 0, len(configMapTypeDefinitions))
	for _, cmTypeDef := range configMapTypeDefinitions {
		daemonSetAndDeployment = append(
			daemonSetAndDeployment,
			Entry(fmt.Sprintf("for the %s", string(cmTypeDef.cmType)), cmTypeDef))
	}

	Describe("renders exporters", func() {

		DescribeTable("should render the default OTLP exporter without other exporters", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(1))

			exporter := exporters["otlp_grpc/dash0/default"]
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

		}, daemonSetAndDeployment)

		DescribeTable("should render the default OTLP exporter with custom dataset", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporterWithCustomDs(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(1))

			exporter := exporters["otlp_grpc/dash0/default"]
			Expect(exporter).ToNot(BeNil())
			dash0OtlpExporter := exporter.(map[string]interface{})
			Expect(dash0OtlpExporter).ToNot(BeNil())
			Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
			headersRaw := dash0OtlpExporter["headers"]
			Expect(headersRaw).ToNot(BeNil())
			headers := headersRaw.(map[string]interface{})
			Expect(headers).To(HaveLen(2))
			Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))
			Expect(headers[util.Dash0DatasetHeaderName]).To(Equal(DatasetCustomTest))
			Expect(dash0OtlpExporter["encoding"]).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should render a debug exporter in development mode", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				DevelopmentMode: true,
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

			exporter := exporters["otlp_grpc/dash0/default"]
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

		}, daemonSetAndDeployment)

		DescribeTable("should render a debug exporter with verbosity: detailed when requested", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
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

		}, daemonSetAndDeployment)

		DescribeTable("should not render the pprof extension when not requested", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:   OperatorNamespace,
				NamePrefix:          namePrefix,
				Exporters:           cmTestSingleDefaultOtlpExporter(),
				EnableProfExtension: false,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			extensionsRaw := collectorConfig["extensions"]
			Expect(extensionsRaw).ToNot(BeNil())
			extensions := extensionsRaw.(map[string]interface{})
			Expect(maps.Keys(extensions)).ToNot(ContainElement("pprof"))

			serviceExtensions := ReadFromMap(collectorConfig, []string{"service", "extensions"})
			Expect(serviceExtensions).To(Not(BeNil()))
			Expect(serviceExtensions).ToNot(ContainElement("pprof"))
		}, daemonSetAndDeployment)

		DescribeTable("should render the pprof extension when requested", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:   OperatorNamespace,
				NamePrefix:          namePrefix,
				Exporters:           cmTestSingleDefaultOtlpExporter(),
				EnableProfExtension: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			extensionsRaw := collectorConfig["extensions"]
			Expect(extensionsRaw).ToNot(BeNil())
			extensions := extensionsRaw.(map[string]interface{})
			Expect(maps.Keys(extensions)).To(ContainElement("pprof"))

			serviceExtensions := ReadFromMap(collectorConfig, []string{"service", "extensions"})
			Expect(serviceExtensions).To(Not(BeNil()))
			Expect(serviceExtensions).To(ContainElement("pprof"))
		}, daemonSetAndDeployment)

		DescribeTable("should render multiple default OTLP exporters (Dash0 + gRPC)", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestDash0AndGrpcExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(2))

			// Verify Dash0 exporter
			dash0Exporter := exporters["otlp_grpc/dash0/default"]
			Expect(dash0Exporter).ToNot(BeNil())
			dash0OtlpExporter := dash0Exporter.(map[string]interface{})
			Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))
			headersRaw := dash0OtlpExporter["headers"]
			Expect(headersRaw).ToNot(BeNil())
			headers := headersRaw.(map[string]interface{})
			Expect(headers[util.AuthorizationHeaderName]).To(Equal(bearerWithAuthToken))

			// Verify gRPC exporter
			grpcExporter := exporters["otlp_grpc/default"]
			Expect(grpcExporter).ToNot(BeNil())
			grpcOtlpExporter := grpcExporter.(map[string]interface{})
			Expect(grpcOtlpExporter["endpoint"]).To(Equal(EndpointGrpcTest))
			grpcHeadersRaw := grpcOtlpExporter["headers"]
			Expect(grpcHeadersRaw).ToNot(BeNil())
			grpcHeaders := grpcHeadersRaw.(map[string]interface{})
			Expect(grpcHeaders["Key"]).To(Equal("Value"))
		}, daemonSetAndDeployment)

		DescribeTable("should render multiple default OTLP exporters (Dash0 + HTTP)", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestDash0AndHttpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(2))

			// Verify Dash0 exporter
			dash0Exporter := exporters["otlp_grpc/dash0/default"]
			Expect(dash0Exporter).ToNot(BeNil())
			dash0OtlpExporter := dash0Exporter.(map[string]interface{})
			Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))

			// Verify HTTP exporter
			httpExporter := exporters["otlp_http/default/proto"]
			Expect(httpExporter).ToNot(BeNil())
			httpOtlpExporter := httpExporter.(map[string]interface{})
			Expect(httpOtlpExporter["endpoint"]).To(Equal(EndpointHttpTest))
			Expect(httpOtlpExporter["encoding"]).To(Equal("proto"))
			httpHeadersRaw := httpOtlpExporter["headers"]
			Expect(httpHeadersRaw).ToNot(BeNil())
			httpHeaders := httpHeadersRaw.(map[string]interface{})
			Expect(httpHeaders["Key"]).To(Equal("Value"))
		}, daemonSetAndDeployment)

		DescribeTable("should render multiple default OTLP exporters (Dash0 + gRPC + HTTP)", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestDash0GrpcAndHttpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(3))

			// Verify Dash0 exporter
			dash0Exporter := exporters["otlp_grpc/dash0/default"]
			Expect(dash0Exporter).ToNot(BeNil())
			dash0OtlpExporter := dash0Exporter.(map[string]interface{})
			Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0Test))

			// Verify gRPC exporter
			grpcExporter := exporters["otlp_grpc/default"]
			Expect(grpcExporter).ToNot(BeNil())
			grpcOtlpExporter := grpcExporter.(map[string]interface{})
			Expect(grpcOtlpExporter["endpoint"]).To(Equal(EndpointGrpcTest))

			// Verify HTTP exporter
			httpExporter := exporters["otlp_http/default/proto"]
			Expect(httpExporter).ToNot(BeNil())
			httpOtlpExporter := httpExporter.(map[string]interface{})
			Expect(httpOtlpExporter["endpoint"]).To(Equal(EndpointHttpTest))
			Expect(httpOtlpExporter["encoding"]).To(Equal("proto"))
		}, daemonSetAndDeployment)

		DescribeTable("should render gRPC exporter with insecure flag", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestGrpcExporterWithInsecure(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(1))

			// Verify gRPC exporter with insecure flag
			grpcExporter := exporters["otlp_grpc/default"]
			Expect(grpcExporter).ToNot(BeNil())
			grpcOtlpExporter := grpcExporter.(map[string]interface{})
			Expect(grpcOtlpExporter["endpoint"]).To(Equal(grpcEndpointTest))

			// Verify tls config with insecure = true
			tlsRaw := grpcOtlpExporter["tls"]
			Expect(tlsRaw).ToNot(BeNil())
			tls := tlsRaw.(map[string]interface{})
			Expect(tls["insecure"]).To(Equal(true))
		}, daemonSetAndDeployment)

		DescribeTable("should render HTTP exporter with insecure endpoint (http://)", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestHttpExporterWithInsecure(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(1))

			// Verify HTTP exporter with insecure endpoint
			httpExporter := exporters["otlp_http/default/proto"]
			Expect(httpExporter).ToNot(BeNil())
			httpOtlpExporter := httpExporter.(map[string]interface{})
			Expect(httpOtlpExporter["endpoint"]).To(Equal(httpInsecureEndpointTest))
			Expect(httpOtlpExporter["encoding"]).To(Equal("proto"))
		}, daemonSetAndDeployment)

		DescribeTable("should render namespaced OTLP exporters", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(3))

			// Verify default Dash0 exporter
			dash0Exporter := exporters["otlp_grpc/dash0/default"]
			Expect(dash0Exporter).ToNot(BeNil())

			// Verify namespaced gRPC exporter for namespace1
			grpcExporterNs1 := exporters["otlp_grpc/ns/"+namespace1]
			Expect(grpcExporterNs1).ToNot(BeNil())
			grpcOtlpExporter := grpcExporterNs1.(map[string]interface{})
			Expect(grpcOtlpExporter["endpoint"]).To(Equal(EndpointGrpcTest))

			// Verify namespaced HTTP exporter for namespace2
			httpExporterNs2 := exporters["otlp_http/ns/"+namespace2+"/proto"]
			Expect(httpExporterNs2).ToNot(BeNil())
			httpOtlpExporter := httpExporterNs2.(map[string]interface{})
			Expect(httpOtlpExporter["endpoint"]).To(Equal(EndpointHttpTest))
			Expect(httpOtlpExporter["encoding"]).To(Equal("proto"))
		}, daemonSetAndDeployment)

		DescribeTable("should render multiple namespaced OTLP exporters", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestMultipleNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			exportersRaw := collectorConfig["exporters"]
			Expect(exportersRaw).ToNot(BeNil())
			exporters := exportersRaw.(map[string]interface{})
			Expect(exporters).To(HaveLen(4))

			// Verify default Dash0 exporter
			dash0Exporter := exporters["otlp_grpc/dash0/default"]
			Expect(dash0Exporter).ToNot(BeNil())

			// Verify namespace1 has both gRPC and HTTP exporters
			grpcExporterNs1 := exporters["otlp_grpc/ns/"+namespace1]
			Expect(grpcExporterNs1).ToNot(BeNil())

			httpExporterNs1 := exporters["otlp_http/ns/"+namespace1+"/proto"]
			Expect(httpExporterNs1).ToNot(BeNil())

			// Verify namespace2 has a Dash0 exporter
			dash0ExporterNs2 := exporters["otlp_grpc/dash0/ns/"+namespace2]
			Expect(dash0ExporterNs2).ToNot(BeNil())
			dash0OtlpExporter := dash0ExporterNs2.(map[string]interface{})
			Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointDash0TestAlternative))
		}, daemonSetAndDeployment)

		It("should render routing connectors when namespaced exporters are configured [DaemonSet])", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)

			// Verify routing connectors are created
			connectorsRaw := collectorConfig["connectors"]
			Expect(connectorsRaw).ToNot(BeNil())
			connectors := connectorsRaw.(map[string]interface{})

			// Verify routing connector for traces
			routingTracesRaw := connectors["routing/traces"]
			Expect(routingTracesRaw).ToNot(BeNil())
			routingTraces := routingTracesRaw.(map[string]interface{})
			Expect(routingTraces["error_mode"]).To(Equal("ignore"))
			defaultPipelinesTraces := routingTraces["default_pipelines"].([]interface{})
			Expect(defaultPipelinesTraces).To(ContainElement("traces/export/default"))

			// Verify routing connector for metrics
			routingMetricsRaw := connectors["routing/metrics"]
			Expect(routingMetricsRaw).ToNot(BeNil())
			routingMetrics := routingMetricsRaw.(map[string]interface{})
			Expect(routingMetrics["error_mode"]).To(Equal("ignore"))
			defaultPipelinesMetrics := routingMetrics["default_pipelines"].([]interface{})
			Expect(defaultPipelinesMetrics).To(ContainElement("metrics/export/default"))

			// Verify routing connector for logs
			routingLogsRaw := connectors["routing/logs"]
			Expect(routingLogsRaw).ToNot(BeNil())
			routingLogs := routingLogsRaw.(map[string]interface{})
			Expect(routingLogs["error_mode"]).To(Equal("ignore"))
			defaultPipelinesLogs := routingLogs["default_pipelines"].([]interface{})
			Expect(defaultPipelinesLogs).To(ContainElement("logs/export/default"))
		})

		It("should render routing connectors when namespaced exporters are configured [Deployment])", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)

			// Verify routing connectors are created
			connectorsRaw := collectorConfig["connectors"]
			Expect(connectorsRaw).ToNot(BeNil())
			connectors := connectorsRaw.(map[string]interface{})

			// Verify routing connector for metrics
			routingMetricsRaw := connectors["routing/metrics"]
			Expect(routingMetricsRaw).ToNot(BeNil())
			routingMetrics := routingMetricsRaw.(map[string]interface{})
			Expect(routingMetrics["error_mode"]).To(Equal("ignore"))
			defaultPipelinesMetrics := routingMetrics["default_pipelines"].([]interface{})
			Expect(defaultPipelinesMetrics).To(ContainElement("metrics/export/default"))

			// Verify routing connector for logs
			routingLogsRaw := connectors["routing/logs"]
			Expect(routingLogsRaw).ToNot(BeNil())
			routingLogs := routingLogsRaw.(map[string]interface{})
			Expect(routingLogs["error_mode"]).To(Equal("ignore"))
			defaultPipelinesLogs := routingLogs["default_pipelines"].([]interface{})
			Expect(defaultPipelinesLogs).To(ContainElement("logs/export/default"))
		})

		It("should render routing table entries with correct namespace conditions [DaemonSet])", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)

			connectorsRaw := collectorConfig["connectors"]
			Expect(connectorsRaw).ToNot(BeNil())
			connectors := connectorsRaw.(map[string]interface{})

			// Verify routing table entries for traces
			routingTraces := connectors["routing/traces"].(map[string]interface{})
			tracesTable := routingTraces["table"].([]interface{})
			Expect(tracesTable).To(HaveLen(2)) // namespace1 and namespace2
			verifyRoutingTableEntry(tracesTable, namespace1, "traces/export/ns/"+namespace1)
			verifyRoutingTableEntry(tracesTable, namespace2, "traces/export/ns/"+namespace2)

			// Verify routing table entries for metrics
			routingMetrics := connectors["routing/metrics"].(map[string]interface{})
			metricsTable := routingMetrics["table"].([]interface{})
			Expect(metricsTable).To(HaveLen(2))
			verifyRoutingTableEntry(metricsTable, namespace1, "metrics/export/ns/"+namespace1)
			verifyRoutingTableEntry(metricsTable, namespace2, "metrics/export/ns/"+namespace2)

			// Verify routing table entries for logs
			routingLogs := connectors["routing/logs"].(map[string]interface{})
			logsTable := routingLogs["table"].([]interface{})
			Expect(logsTable).To(HaveLen(2))
			verifyRoutingTableEntry(logsTable, namespace1, "logs/export/ns/"+namespace1)
			verifyRoutingTableEntry(logsTable, namespace2, "logs/export/ns/"+namespace2)
		})

		It("should render routing table entries with correct namespace conditions [Deployment])", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)

			connectorsRaw := collectorConfig["connectors"]
			Expect(connectorsRaw).ToNot(BeNil())
			connectors := connectorsRaw.(map[string]interface{})

			// Verify routing table entries for metrics
			routingMetrics := connectors["routing/metrics"].(map[string]interface{})
			metricsTable := routingMetrics["table"].([]interface{})
			Expect(metricsTable).To(HaveLen(2))
			verifyRoutingTableEntry(metricsTable, namespace1, "metrics/export/ns/"+namespace1)
			verifyRoutingTableEntry(metricsTable, namespace2, "metrics/export/ns/"+namespace2)

			// Verify routing table entries for logs
			routingLogs := connectors["routing/logs"].(map[string]interface{})
			logsTable := routingLogs["table"].([]interface{})
			Expect(logsTable).To(HaveLen(2))
			verifyRoutingTableEntry(logsTable, namespace1, "logs/export/ns/"+namespace1)
			verifyRoutingTableEntry(logsTable, namespace2, "logs/export/ns/"+namespace2)
		})

		It("should render namespace-specific export pipelines [DaemonSet])", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify namespace-specific traces export pipelines
			tracesExportNs1Receivers := readPipelineReceivers(pipelines, "traces/export/ns/"+namespace1)
			Expect(tracesExportNs1Receivers).To(ContainElement("routing/traces"))
			tracesExportNs1Exporters := readPipelineExporters(pipelines, "traces/export/ns/"+namespace1)
			Expect(tracesExportNs1Exporters).To(ContainElement("otlp_grpc/ns/" + namespace1))

			tracesExportNs2Receivers := readPipelineReceivers(pipelines, "traces/export/ns/"+namespace2)
			Expect(tracesExportNs2Receivers).To(ContainElement("routing/traces"))
			tracesExportNs2Exporters := readPipelineExporters(pipelines, "traces/export/ns/"+namespace2)
			Expect(tracesExportNs2Exporters).To(ContainElement("otlp_http/ns/" + namespace2 + "/proto"))

			// Verify namespace-specific metrics export pipelines
			metricsExportNs1Receivers := readPipelineReceivers(pipelines, "metrics/export/ns/"+namespace1)
			Expect(metricsExportNs1Receivers).To(ContainElement("routing/metrics"))
			metricsExportNs1Exporters := readPipelineExporters(pipelines, "metrics/export/ns/"+namespace1)
			Expect(metricsExportNs1Exporters).To(ContainElement("otlp_grpc/ns/" + namespace1))

			metricsExportNs2Receivers := readPipelineReceivers(pipelines, "metrics/export/ns/"+namespace2)
			Expect(metricsExportNs2Receivers).To(ContainElement("routing/metrics"))
			metricsExportNs2Exporters := readPipelineExporters(pipelines, "metrics/export/ns/"+namespace2)
			Expect(metricsExportNs2Exporters).To(ContainElement("otlp_http/ns/" + namespace2 + "/proto"))

			// Verify namespace-specific logs export pipelines
			logsExportNs1Receivers := readPipelineReceivers(pipelines, "logs/export/ns/"+namespace1)
			Expect(logsExportNs1Receivers).To(ContainElement("routing/logs"))
			logsExportNs1Exporters := readPipelineExporters(pipelines, "logs/export/ns/"+namespace1)
			Expect(logsExportNs1Exporters).To(ContainElement("otlp_grpc/ns/" + namespace1))

			logsExportNs2Receivers := readPipelineReceivers(pipelines, "logs/export/ns/"+namespace2)
			Expect(logsExportNs2Receivers).To(ContainElement("routing/logs"))
			logsExportNs2Exporters := readPipelineExporters(pipelines, "logs/export/ns/"+namespace2)
			Expect(logsExportNs2Exporters).To(ContainElement("otlp_http/ns/" + namespace2 + "/proto"))
		})

		It("should render namespace-specific export pipelines [Deployment])", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify namespace-specific metrics export pipelines
			metricsExportNs1Receivers := readPipelineReceivers(pipelines, "metrics/export/ns/"+namespace1)
			Expect(metricsExportNs1Receivers).To(ContainElement("routing/metrics"))
			metricsExportNs1Exporters := readPipelineExporters(pipelines, "metrics/export/ns/"+namespace1)
			Expect(metricsExportNs1Exporters).To(ContainElement("otlp_grpc/ns/" + namespace1))

			metricsExportNs2Receivers := readPipelineReceivers(pipelines, "metrics/export/ns/"+namespace2)
			Expect(metricsExportNs2Receivers).To(ContainElement("routing/metrics"))
			metricsExportNs2Exporters := readPipelineExporters(pipelines, "metrics/export/ns/"+namespace2)
			Expect(metricsExportNs2Exporters).To(ContainElement("otlp_http/ns/" + namespace2 + "/proto"))

			// Verify namespace-specific logs export pipelines
			logsExportNs1Receivers := readPipelineReceivers(pipelines, "logs/export/ns/"+namespace1)
			Expect(logsExportNs1Receivers).To(ContainElement("routing/logs"))
			logsExportNs1Exporters := readPipelineExporters(pipelines, "logs/export/ns/"+namespace1)
			Expect(logsExportNs1Exporters).To(ContainElement("otlp_grpc/ns/" + namespace1))

			logsExportNs2Receivers := readPipelineReceivers(pipelines, "logs/export/ns/"+namespace2)
			Expect(logsExportNs2Receivers).To(ContainElement("routing/logs"))
			logsExportNs2Exporters := readPipelineExporters(pipelines, "logs/export/ns/"+namespace2)
			Expect(logsExportNs2Exporters).To(ContainElement("otlp_http/ns/" + namespace2 + "/proto"))
		})

		It("should wire common-processors pipelines to routing connectors when namespaced exporters exist [DaemonSet]", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify traces/common-processors exports to routing/traces
			tracesCommonProcessorsExporters := readPipelineExporters(pipelines, "traces/common-processors")
			Expect(tracesCommonProcessorsExporters).To(ContainElement("routing/traces"))
			Expect(tracesCommonProcessorsExporters).ToNot(ContainElement("forward/traces-default-exporter"))

			// Verify metrics/common-processors exports to routing/metrics
			metricsCommonProcessorsExporters := readPipelineExporters(pipelines, "metrics/common-processors")
			Expect(metricsCommonProcessorsExporters).To(ContainElement("routing/metrics"))
			Expect(metricsCommonProcessorsExporters).ToNot(ContainElement("forward/metrics-default-exporter"))

			// Verify logs/common-processors exports to routing/logs
			logsCommonProcessorsExporters := readPipelineExporters(pipelines, "logs/common-processors")
			Expect(logsCommonProcessorsExporters).To(ContainElement("routing/logs"))
			Expect(logsCommonProcessorsExporters).ToNot(ContainElement("forward/logs-default-exporter"))
		})

		It("should wire common-processors pipelines to routing connectors when namespaced exporters exist [Deployment]", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify metrics/common-processors exports to routing/metrics
			metricsCommonProcessorsExporters := readPipelineExporters(pipelines, "metrics/common-processors")
			Expect(metricsCommonProcessorsExporters).To(ContainElement("routing/metrics"))
			Expect(metricsCommonProcessorsExporters).ToNot(ContainElement("forward/metrics-default-exporter"))

			// Verify logs/common-processors exports to routing/logs
			logsCommonProcessorsExporters := readPipelineExporters(pipelines, "logs/k8sevents")
			Expect(logsCommonProcessorsExporters).To(ContainElement("routing/logs"))
			Expect(logsCommonProcessorsExporters).ToNot(ContainElement("forward/logs-default-exporter"))
		})

		It("should wire default export pipelines to receive from routing connectors when namespaced exporters exist [DaemonSet]", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify traces/export/default receives from routing/traces
			tracesExportDefaultReceivers := readPipelineReceivers(pipelines, "traces/export/default")
			Expect(tracesExportDefaultReceivers).To(ContainElement("routing/traces"))
			Expect(tracesExportDefaultReceivers).ToNot(ContainElement("forward/traces-default-exporter"))

			// Verify metrics/export/default receives from routing/metrics
			metricsExportDefaultReceivers := readPipelineReceivers(pipelines, "metrics/export/default")
			Expect(metricsExportDefaultReceivers).To(ContainElement("routing/metrics"))
			Expect(metricsExportDefaultReceivers).ToNot(ContainElement("forward/metrics-default-exporter"))

			// Verify logs/export/default receives from routing/logs
			logsExportDefaultReceivers := readPipelineReceivers(pipelines, "logs/export/default")
			Expect(logsExportDefaultReceivers).To(ContainElement("routing/logs"))
			Expect(logsExportDefaultReceivers).ToNot(ContainElement("forward/logs-default-exporter"))
		})

		It("should wire default export pipelines to receive from routing connectors when namespaced exporters exist [Deployment]", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestNamespacedOtlpExporters(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify metrics/export/default receives from routing/metrics
			metricsExportDefaultReceivers := readPipelineReceivers(pipelines, "metrics/export/default")
			Expect(metricsExportDefaultReceivers).To(ContainElement("routing/metrics"))
			Expect(metricsExportDefaultReceivers).ToNot(ContainElement("forward/metrics-default-exporter"))

			// Verify logs/export/default receives from routing/logs
			logsExportDefaultReceivers := readPipelineReceivers(pipelines, "logs/export/default")
			Expect(logsExportDefaultReceivers).To(ContainElement("routing/logs"))
			Expect(logsExportDefaultReceivers).ToNot(ContainElement("forward/logs-default-exporter"))
		})

		DescribeTable("should not render routing connectors when no namespaced exporters exist", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)

			// Verify routing connectors are NOT created
			connectorsRaw := collectorConfig["connectors"]
			Expect(connectorsRaw).ToNot(BeNil())
			connectors := connectorsRaw.(map[string]interface{})
			Expect(connectors["routing/traces"]).To(BeNil())
			Expect(connectors["routing/metrics"]).To(BeNil())
			Expect(connectors["routing/logs"]).To(BeNil())

			// Verify forward connectors are used instead
			Expect(connectors["forward/traces-default-exporter"]).ToNot(BeNil())
			Expect(connectors["forward/metrics-default-exporter"]).ToNot(BeNil())
			Expect(connectors["forward/logs-default-exporter"]).ToNot(BeNil())
		}, daemonSetAndDeployment)

		It("should use forward connectors in pipelines when no namespaced exporters exist [DaemonSet]", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			// Verify common-processors pipelines export to forward connectors
			tracesCommonProcessorsExporters := readPipelineExporters(pipelines, "traces/common-processors")
			Expect(tracesCommonProcessorsExporters).To(ContainElement("forward/traces-default-exporter"))
			Expect(tracesCommonProcessorsExporters).ToNot(ContainElement("routing/traces"))

			metricsCommonProcessorsExporters := readPipelineExporters(pipelines, "metrics/common-processors")
			Expect(metricsCommonProcessorsExporters).To(ContainElement("forward/metrics-default-exporter"))
			Expect(metricsCommonProcessorsExporters).ToNot(ContainElement("routing/metrics"))

			logsCommonProcessorsExporters := readPipelineExporters(pipelines, "logs/common-processors")
			Expect(logsCommonProcessorsExporters).To(ContainElement("forward/logs-default-exporter"))
			Expect(logsCommonProcessorsExporters).ToNot(ContainElement("routing/logs"))

			// Verify default export pipelines receive from forward connectors
			tracesExportDefaultReceivers := readPipelineReceivers(pipelines, "traces/export/default")
			Expect(tracesExportDefaultReceivers).To(ContainElement("forward/traces-default-exporter"))
			Expect(tracesExportDefaultReceivers).ToNot(ContainElement("routing/traces"))

			metricsExportDefaultReceivers := readPipelineReceivers(pipelines, "metrics/export/default")
			Expect(metricsExportDefaultReceivers).To(ContainElement("forward/metrics-default-exporter"))
			Expect(metricsExportDefaultReceivers).ToNot(ContainElement("routing/metrics"))

			logsExportDefaultReceivers := readPipelineReceivers(pipelines, "logs/export/default")
			Expect(logsExportDefaultReceivers).To(ContainElement("forward/logs-default-exporter"))
			Expect(logsExportDefaultReceivers).ToNot(ContainElement("routing/logs"))
		})

		It("should use forward connectors in pipelines when no namespaced exporters exist [Deployment]", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, defaultNamespacesWithEventCollection, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			pipelines := readPipelines(collectorConfig)

			metricsCommonProcessorsExporters := readPipelineExporters(pipelines, "metrics/common-processors")
			Expect(metricsCommonProcessorsExporters).To(ContainElement("forward/metrics-default-exporter"))
			Expect(metricsCommonProcessorsExporters).ToNot(ContainElement("routing/metrics"))

			logsCommonProcessorsExporters := readPipelineExporters(pipelines, "logs/k8sevents")
			Expect(logsCommonProcessorsExporters).To(ContainElement("forward/logs-default-exporter"))
			Expect(logsCommonProcessorsExporters).ToNot(ContainElement("routing/logs"))

			metricsExportDefaultReceivers := readPipelineReceivers(pipelines, "metrics/export/default")
			Expect(metricsExportDefaultReceivers).To(ContainElement("forward/metrics-default-exporter"))
			Expect(metricsExportDefaultReceivers).ToNot(ContainElement("routing/metrics"))

			logsExportDefaultReceivers := readPipelineReceivers(pipelines, "logs/export/default")
			Expect(logsExportDefaultReceivers).To(ContainElement("forward/logs-default-exporter"))
			Expect(logsExportDefaultReceivers).ToNot(ContainElement("routing/logs"))
		})
	})

	DescribeTable("should render batch processor with defaults if SendBatchMaxSize is not requested", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			SendBatchMaxSize: nil,
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		batchProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "batch"})
		Expect(batchProcessorRaw).ToNot(BeNil())
		batchProcessor := batchProcessorRaw.(map[string]interface{})
		Expect(batchProcessor).To(HaveLen(0))
	}, daemonSetAndDeployment)

	DescribeTable("should not set send_batch_max_size on batch processor if requested", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			SendBatchMaxSize: ptr.To(uint32(16384)),
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		batchProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "batch"})
		Expect(batchProcessorRaw).ToNot(BeNil())
		batchProcessor := batchProcessorRaw.(map[string]interface{})
		Expect(batchProcessor).To(HaveLen(1))
		sendBatchMaxSize := ReadFromMap(batchProcessor, []string{"send_batch_max_size"})
		Expect(sendBatchMaxSize).To(Equal(16384))
	}, daemonSetAndDeployment)

	DescribeTable("should not render resource processor if the cluster name has not been set", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			Exporters: cmTestSingleDefaultOtlpExporter(),
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := ReadFromMap(collectorConfig, []string{"processors", "resource/clustername"})
		Expect(resourceProcessor).To(BeNil())
		verifyProcessorDoesNotAppearInAnyPipeline(collectorConfig, "resource/clustername")
	}, daemonSetAndDeployment)

	DescribeTable("should render resource processor with k8s.cluster.name if available", func(cmTypeDef configMapTypeDefinition) {
		configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			ClusterName: "cluster-name",
		}, monitoredNamespaces, nil, nil, false)

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		resourceProcessor := ReadFromMap(collectorConfig, []string{"processors", "resource/clustername"})
		Expect(resourceProcessor).ToNot(BeNil())
		attributes := ReadFromMap(resourceProcessor, []string{"attributes"})
		Expect(attributes).To(HaveLen(1))
		attrs := attributes.([]interface{})
		Expect(attrs[0].(map[string]interface{})["key"]).To(Equal("k8s.cluster.name"))
		Expect(attrs[0].(map[string]interface{})["value"]).To(Equal("cluster-name"))
		Expect(attrs[0].(map[string]interface{})["action"]).To(Equal("insert"))
		pipelines := readPipelines(collectorConfig)
		metricsProcessors := readPipelineProcessors(pipelines, "metrics/common-processors")
		Expect(metricsProcessors).ToNot(BeNil())
		Expect(metricsProcessors).To(ContainElement("resource/clustername"))
	}, daemonSetAndDeployment)

	Describe("should enable/disable kubernetes infrastructure metrics collection and the hostmetrics receiver", func() {
		It("should not render the kubeletstats receiver and hostmetrics if kubernetes infrastructure metrics collection is disabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: false,
				KubeletStatsReceiverConfig:                       KubeletStatsReceiverConfig{Enabled: false},
				UseHostMetricsReceiver:                           false,
			}, nil, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			kubeletstatsReceiver := ReadFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
			Expect(kubeletstatsReceiver).To(BeNil())
			hostmetricsReceiver := ReadFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
			Expect(hostmetricsReceiver).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			metricsReceivers := readPipelineReceivers(pipelines, "metrics/otlp-to-forwarder")
			Expect(metricsReceivers).ToNot(BeNil())
			Expect(metricsReceivers).To(ContainElement("otlp"))
			Expect(metricsReceivers).ToNot(ContainElement("kubeletstats"))
			Expect(metricsReceivers).ToNot(ContainElement("hostmetrics"))
			defaultMetricsExporters := readPipelineExporters(pipelines, "metrics/otlp-to-forwarder")
			Expect(defaultMetricsExporters).To(ContainElement("forward/metrics-processors"))
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
					Exporters:         cmTestSingleDefaultOtlpExporter(),
					KubernetesInfrastructureMetricsCollectionEnabled: true,
					KubeletStatsReceiverConfig:                       testConfig.kubeletStatsReceiverConfig,
					UseHostMetricsReceiver:                           true,
				}, nil, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)
				Expect(err).ToNot(HaveOccurred())
				collectorConfig := parseConfigMapContent(configMap)
				kubeletstatsReceiverRaw := ReadFromMap(collectorConfig, []string{"receivers", "kubeletstats"})
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

				hostmetricsReceiver := ReadFromMap(collectorConfig, []string{"receivers", "hostmetrics"})
				Expect(hostmetricsReceiver).ToNot(BeNil())

				pipelines := readPipelines(collectorConfig)
				metricsReceivers := readPipelineReceivers(pipelines, "metrics/otlp-to-forwarder")
				Expect(metricsReceivers).ToNot(BeNil())
				Expect(metricsReceivers).To(ContainElement("otlp"))
				Expect(metricsReceivers).To(ContainElement("kubeletstats"))
				Expect(metricsReceivers).To(ContainElement("hostmetrics"))
				defaultMetricsExporters := readPipelineExporters(pipelines, "metrics/otlp-to-forwarder")
				Expect(defaultMetricsExporters).To(ContainElement("forward/metrics-processors"))
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

	Describe("should enable/disable the replicaset informer", func() {
		DescribeTable("should configure the k8sattributes processor to not start the replicaset informer if disabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:         OperatorNamespace,
				NamePrefix:                namePrefix,
				Exporters:                 cmTestSingleDefaultOtlpExporter(),
				DisableReplicasetInformer: true,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			deploymentNameFromReplicasetRaw := ReadFromMap(k8sAttributesProcessor, []string{"extract", "deployment_name_from_replicaset"})
			Expect(deploymentNameFromReplicasetRaw).ToNot(BeNil())
			deploymentNameFromReplicaset := deploymentNameFromReplicasetRaw.(bool)
			Expect(deploymentNameFromReplicaset).To(BeTrue())
			metadataListRaw := ReadFromMap(k8sAttributesProcessor, []string{"extract", "metadata"})
			Expect(metadataListRaw).ToNot(BeNil())
			metadataList := metadataListRaw.([]interface{})
			Expect(metadataList).ToNot(ContainElement("k8s.deployment.uid"))
		}, daemonSetAndDeployment)

		DescribeTable("should configure the k8sattributes processor to use the replicaset informer by default", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace:         OperatorNamespace,
				NamePrefix:                namePrefix,
				Exporters:                 cmTestSingleDefaultOtlpExporter(),
				DisableReplicasetInformer: false,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			deploymentNameFromReplicasetRaw := ReadFromMap(k8sAttributesProcessor, []string{"extract", "deployment_name_from_replicaset"})
			Expect(deploymentNameFromReplicasetRaw).To(BeNil())
			metadataListRaw := ReadFromMap(k8sAttributesProcessor, []string{"extract", "metadata"})
			Expect(metadataListRaw).ToNot(BeNil())
			metadataList := metadataListRaw.([]interface{})
			Expect(metadataList).To(ContainElement("k8s.deployment.uid"))
		}, daemonSetAndDeployment)
	})

	Describe("should enable/disable collecting labels and annotations", func() {
		DescribeTable("should not render the label/annotation collection snippet if disabled for namespaces and pods", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				CollectPodLabelsAndAnnotationsEnabled:            false,
				CollectNamespaceLabelsAndAnnotationsEnabled:      false,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).To(BeNil())
			annotationsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should collect labels and annotation from namespaces and pods if both are enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				CollectPodLabelsAndAnnotationsEnabled:            true,
				CollectNamespaceLabelsAndAnnotationsEnabled:      true,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).ToNot(BeNil())
			Expect(labelsSnippet).To(HaveLen(2))
			annotationsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).ToNot(BeNil())
			Expect(annotationsSnippet).To(HaveLen(2))
			namespaceLabelTag := ReadFromMap(labelsSnippet, []string{"0", "tag_name"})
			Expect(namespaceLabelTag).To(Equal("k8s.namespace.label.$$1"))
			podLabelTag := ReadFromMap(labelsSnippet, []string{"1", "tag_name"})
			Expect(podLabelTag).To(Equal("k8s.pod.label.$$1"))
			namespaceAnnotationTag := ReadFromMap(annotationsSnippet, []string{"0", "tag_name"})
			Expect(namespaceAnnotationTag).To(Equal("k8s.namespace.annotation.$$1"))
			podAnnotationTag := ReadFromMap(annotationsSnippet, []string{"1", "tag_name"})
			Expect(podAnnotationTag).To(Equal("k8s.pod.annotation.$$1"))
		}, daemonSetAndDeployment)

		DescribeTable("should collect only namespace labels/annotations when only namespace collection is enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				CollectPodLabelsAndAnnotationsEnabled:            false,
				CollectNamespaceLabelsAndAnnotationsEnabled:      true,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).ToNot(BeNil())
			Expect(labelsSnippet).To(HaveLen(1))
			annotationsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).ToNot(BeNil())
			Expect(annotationsSnippet).To(HaveLen(1))
			namespaceLabelTag := ReadFromMap(labelsSnippet, []string{"0", "tag_name"})
			Expect(namespaceLabelTag).To(Equal("k8s.namespace.label.$$1"))
			namespaceAnnotationTag := ReadFromMap(annotationsSnippet, []string{"0", "tag_name"})
			Expect(namespaceAnnotationTag).To(Equal("k8s.namespace.annotation.$$1"))
		}, daemonSetAndDeployment)

		DescribeTable("should collect only pod labels/annotations when only pod collection is enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				CollectPodLabelsAndAnnotationsEnabled:            true,
				CollectNamespaceLabelsAndAnnotationsEnabled:      false,
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			k8sAttributesProcessorRaw := ReadFromMap(collectorConfig, []string{"processors", "k8sattributes"})
			Expect(k8sAttributesProcessorRaw).ToNot(BeNil())
			k8sAttributesProcessor := k8sAttributesProcessorRaw.(map[string]interface{})
			labelsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "labels"})
			Expect(labelsSnippet).ToNot(BeNil())
			Expect(labelsSnippet).To(HaveLen(1))
			annotationsSnippet := ReadFromMap(k8sAttributesProcessor, []string{"extract", "annotations"})
			Expect(annotationsSnippet).ToNot(BeNil())
			Expect(annotationsSnippet).To(HaveLen(1))
			namespaceLabelTag := ReadFromMap(labelsSnippet, []string{"0", "tag_name"})
			Expect(namespaceLabelTag).To(Equal("k8s.pod.label.$$1"))
			namespaceAnnotationTag := ReadFromMap(annotationsSnippet, []string{"0", "tag_name"})
			Expect(namespaceAnnotationTag).To(Equal("k8s.pod.annotation.$$1"))
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
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
			}, monitoredNamespaces, nil, nil, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			filterProcessor := ReadFromMap(collectorConfig, []string{"processors", "filter/metrics/only_monitored_namespaces"})
			Expect(filterProcessor).ToNot(BeNil())
			filters := ReadFromMap(filterProcessor, []string{"metrics", "metric"})
			Expect(filters).To(HaveLen(1))
			filterString := filters.([]interface{})[0].(string)
			Expect(filterString).To(Equal(`resource.attributes["k8s.namespace.name"] != nil and resource.attributes["k8s.namespace.name"] != "namespace-1" and resource.attributes["k8s.namespace.name"] != "namespace-2"`))
			pipelines := readPipelines(collectorConfig)
			metricsProcessors := readPipelineProcessors(pipelines, "metrics/common-processors")
			Expect(metricsProcessors).ToNot(BeNil())
			Expect(metricsProcessors).To(ContainElement("filter/metrics/only_monitored_namespaces"))
		}, daemonSetAndDeployment)
	})

	Describe("prometheus scraping config", func() {
		var config = &oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
		}

		It("should not render the prometheus scraping config if no namespace has scraping enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, []string{}, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(ReadFromMap(collectorConfig, []string{"receivers", "prometheus"})).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			Expect(pipelines["metrics/prometheus"]).To(BeNil())
		})

		It("should render the prometheus scraping config with all namespaces for which scraping is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace-1", "namespace-2", "namespace-3"},
				nil,
				[]string{"namespace-1", "namespace-2"},
				nil,
				nil,
				emptyTargetAllocatorMtlsConfig,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(ReadFromMap(collectorConfig, []string{"receivers", "prometheus"})).ToNot(BeNil())
			for _, jobName := range []string{
				"dash0-kubernetes-pods-scrape-config",
				"dash0-kubernetes-pods-scrape-config-slow",
			} {
				verifyScrapeJobHasNamespaces(collectorConfig, jobName)
			}

			pipelines := readPipelines(collectorConfig)
			prometheusMetricsReceivers := readPipelineReceivers(pipelines, "metrics/prometheus-to-forwarder")
			Expect(prometheusMetricsReceivers).ToNot(BeNil())
			Expect(prometheusMetricsReceivers).To(ContainElement("prometheus"))
			prometheusMetricsProcessors := readPipelineProcessors(pipelines, "metrics/prometheus-to-forwarder")
			Expect(prometheusMetricsProcessors).ToNot(BeNil())
			Expect(prometheusMetricsProcessors).To(ContainElement("transform/metrics/prometheus_service_attributes"))
			prometheusMetricsExporters := readPipelineExporters(pipelines, "metrics/prometheus-to-forwarder")
			Expect(prometheusMetricsExporters).ToNot(BeNil())
			Expect(prometheusMetricsExporters).To(ContainElement("forward/metrics-processors"))
		})
	})

	Describe("log collection", func() {
		var config = &oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
		}

		It("should not render the filelog receiver if no namespace has log collection enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, []string{}, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(ReadFromMap(collectorConfig, []string{"receivers", "filelog"})).To(BeNil())

			filelogServiceAttributeProcessor :=
				ReadFromMap(collectorConfig, []string{"processors", "transform/logs/filelog_service_attributes"})
			Expect(filelogServiceAttributeProcessor).To(BeNil())

			pipelines := readPipelines(collectorConfig)
			Expect(pipelines["logs/filelog"]).To(BeNil())
			logsCommonProcessorsPipelineProcessors := readPipelineProcessors(pipelines, "logs/common-processors")
			Expect(logsCommonProcessorsPipelineProcessors).ToNot(BeNil())
			Expect(logsCommonProcessorsPipelineProcessors).ToNot(ContainElement("transform/logs/filelog_service_attributes"))
		})

		It("should render the filelog config with all namespaces for which log collection is enabled", func() {
			configMap, err := assembleDaemonSetCollectorConfigMap(
				config,
				[]string{"namespace-1", "namespace-2", "namespace-3"},
				[]string{"namespace-1", "namespace-2"},
				nil,
				nil,
				nil,
				emptyTargetAllocatorMtlsConfig,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			fileLogReceiverRaw := ReadFromMap(collectorConfig, []string{"receivers", "filelog"})
			Expect(fileLogReceiverRaw).ToNot(BeNil())
			fileLogReceiver := fileLogReceiverRaw.(map[string]interface{})
			filePatterns := fileLogReceiver["include"].([]interface{})
			Expect(filePatterns).To(HaveLen(2))
			Expect(filePatterns).To(ContainElement("/var/log/pods/namespace-1_*/*/*.log"))
			Expect(filePatterns).To(ContainElement("/var/log/pods/namespace-2_*/*/*.log"))

			filelogServiceAttributeProcessor :=
				ReadFromMap(collectorConfig, []string{"processors", "transform/logs/filelog_service_attributes"})
			Expect(filelogServiceAttributeProcessor).ToNot(BeNil())

			pipelines := readPipelines(collectorConfig)
			logsFilelogReceivers := readPipelineReceivers(pipelines, "logs/filelog-to-forwarder")
			Expect(logsFilelogReceivers).ToNot(BeNil())
			Expect(logsFilelogReceivers).To(ContainElement("filelog"))
			logsFilelogProcessors := readPipelineProcessors(pipelines, "logs/filelog-to-forwarder")
			Expect(logsFilelogProcessors).ToNot(BeNil())
			logsFilelogExporters := readPipelineExporters(pipelines, "logs/filelog-to-forwarder")
			Expect(logsFilelogExporters).ToNot(BeNil())
			Expect(logsFilelogExporters).To(ContainElement("forward/logs-processors"))

			LogsCommonProcessorsPipelineReceivers := readPipelineReceivers(pipelines, "logs/common-processors")
			Expect(LogsCommonProcessorsPipelineReceivers).ToNot(BeNil())
			Expect(LogsCommonProcessorsPipelineReceivers).To(ContainElement("forward/logs-processors"))
			LogsCommonProcessorsPipelineProcessors := readPipelineProcessors(pipelines, "logs/common-processors")
			Expect(LogsCommonProcessorsPipelineProcessors).ToNot(BeNil())
			Expect(LogsCommonProcessorsPipelineProcessors).To(ContainElement("transform/logs/filelog_service_attributes"))
		})
	})

	Describe("Kubernetes infrastructure metrics collection", func() {
		It("should not render the k8s_cluster receiver and the associated pipeline if Kubernetes infrastructure metrics collection is disabled", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(
				&oTelColConfig{
					OperatorNamespace: OperatorNamespace,
					NamePrefix:        namePrefix,
					Exporters:         cmTestSingleDefaultOtlpExporter(),
					KubernetesInfrastructureMetricsCollectionEnabled: false,
				},
				[]string{
					"namespace",
				},
				[]string{
					"namespace",
				},
				nil,
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(ReadFromMap(collectorConfig, []string{"receivers", "k8s_cluster"})).To(BeNil())
			Expect(ReadFromMap(collectorConfig, []string{
				"processors",
				"filter/metrics/only_monitored_namespaces",
			})).To(BeNil())
			Expect(ReadFromMap(collectorConfig, []string{
				"processors",
				"filter/drop-replicaset-metrics-zero-value",
			})).To(BeNil())
			pipelines := readPipelines(collectorConfig)
			Expect(pipelines["metrics/common-processors"]).To(BeNil())
		})

		DescribeTable("should render the k8s_cluster receiver and the associated pipeline if Kubernetes infrastructure metrics collection is enabled",
			func(k8sEventCollectionEnabledForAtLeastOneNamespace bool) {
				var namespacesWithEventCollection []string
				if k8sEventCollectionEnabledForAtLeastOneNamespace {
					namespacesWithEventCollection = []string{"namespace-1"}
				}
				configMap, err := assembleDeploymentCollectorConfigMap(
					&oTelColConfig{
						OperatorNamespace: OperatorNamespace,
						NamePrefix:        namePrefix,
						Exporters:         cmTestSingleDefaultOtlpExporter(),
						KubernetesInfrastructureMetricsCollectionEnabled: true,
					},
					[]string{"namespace-1"},
					namespacesWithEventCollection,
					nil,
					nil,
					false,
				)
				Expect(err).ToNot(HaveOccurred())
				collectorConfig := parseConfigMapContent(configMap)
				Expect(ReadFromMap(collectorConfig, []string{"receivers", "k8s_cluster"})).ToNot(BeNil())
				Expect(ReadFromMap(collectorConfig, []string{
					"processors",
					"filter/metrics/only_monitored_namespaces",
				})).ToNot(BeNil())
				Expect(ReadFromMap(collectorConfig, []string{
					"processors",
					"filter/drop-replicaset-metrics-zero-value",
				})).ToNot(BeNil())
				pipelines := readPipelines(collectorConfig)
				Expect(pipelines["metrics/common-processors"]).NotTo(BeNil())
			},
			Entry("without K8s event collection", false),
			Entry("together with K8s event collection", true),
		)
	})

	Describe("event collection", func() {
		It("should not render the k8s_event receiver if no namespace has event collection enabled", func() {
			configMap, err := assembleDeploymentCollectorConfigMap(
				&oTelColConfig{
					OperatorNamespace: OperatorNamespace,
					NamePrefix:        namePrefix,
					Exporters:         cmTestSingleDefaultOtlpExporter(),
					KubernetesInfrastructureMetricsCollectionEnabled: true,
				},
				[]string{},
				[]string{},
				nil,
				nil,
				false,
			)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(ReadFromMap(collectorConfig, []string{"receivers", "k8s_events"})).To(BeNil())
			Expect(ReadFromMap(collectorConfig, []string{"processors", "transform/k8s_events"})).To(BeNil())
			pipelines := readPipelines(collectorConfig)
			Expect(pipelines["logs/k8sevents"]).To(BeNil())
		})

		DescribeTable("should render the k8s_events receiver with all namespaces for which event collection is enabled",
			func(kubernetesInfrastructureMetricsCollectionEnabled bool) {
				configMap, err := assembleDeploymentCollectorConfigMap(
					&oTelColConfig{
						OperatorNamespace: OperatorNamespace,
						NamePrefix:        namePrefix,
						Exporters:         cmTestSingleDefaultOtlpExporter(),
						KubernetesInfrastructureMetricsCollectionEnabled: kubernetesInfrastructureMetricsCollectionEnabled,
					},
					[]string{"namespace-1", "namespace-2", "namespace-3"},
					[]string{"namespace-1", "namespace-2"},
					nil,
					nil,
					false,
				)
				Expect(err).ToNot(HaveOccurred())
				collectorConfig := parseConfigMapContent(configMap)
				k8sEventsReceiverRaw := ReadFromMap(collectorConfig, []string{"receivers", "k8s_events"})
				Expect(k8sEventsReceiverRaw).ToNot(BeNil())
				k8sEventsReceiver := k8sEventsReceiverRaw.(map[string]interface{})
				namespaces := k8sEventsReceiver["namespaces"].([]interface{})
				Expect(namespaces).To(HaveLen(2))
				Expect(namespaces).To(ContainElement("namespace-1"))
				Expect(namespaces).To(ContainElement("namespace-2"))

				Expect(ReadFromMap(collectorConfig, []string{"processors", "transform/k8s_events"})).ToNot(BeNil())

				pipelines := readPipelines(collectorConfig)
				Expect(pipelines["logs/k8sevents"]).NotTo(BeNil())
			},
			Entry("without Kubernetes infra metrics collection", false),
			Entry("together with Kubernetes infra metrics collection", true),
		)
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
								Filter: dash0common.Filter{
									ErrorMode: dash0common.FilterTransformErrorModeIgnore,
									Traces: &dash0common.TraceFilter{
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
								Filter: dash0common.Filter{
									ErrorMode: dash0common.FilterTransformErrorModeIgnore,
									Metrics: &dash0common.MetricFilter{
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
								Filter: dash0common.Filter{
									ErrorMode: dash0common.FilterTransformErrorModeIgnore,
									Logs: &dash0common.LogFilter{
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
								Filter: dash0common.Filter{
									ErrorMode: dash0common.FilterTransformErrorModeIgnore,
									Traces: &dash0common.TraceFilter{
										SpanFilter:      []string{"span condition 1", "span condition 2"},
										SpanEventFilter: []string{"span event condition 1", "span event condition 2"},
									},
									Metrics: &dash0common.MetricFilter{
										MetricFilter:    []string{"metric condition 1", "metric condition 2"},
										DataPointFilter: []string{"data point condition 1", "data point condition 2"},
									},
									Logs: &dash0common.LogFilter{
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
					Exporters:         cmTestSingleDefaultOtlpExporter(),
					KubernetesInfrastructureMetricsCollectionEnabled: true,
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
				filterProcessorRaw := ReadFromMap(
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
					filterConditionsRaw := ReadFromMap(filterProcessor, []string{string(signal), string(objectType)})
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

				commonProcessorsProcessors := readPipelineProcessors(pipelines, fmt.Sprintf("%s/common-processors", signal))
				Expect(commonProcessorsProcessors).To(ContainElements(filterProcessorName))
			}

			for _, signal := range expectations.signalsWithoutFilters {
				filterProcessorName := fmt.Sprintf("filter/%s/custom_telemetry_filter", signal)
				filterProcessorRaw := ReadFromMap(
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
			errorModes []dash0common.FilterTransformErrorMode
			expected   dash0common.FilterTransformErrorMode
		}

		DescribeTable("filter processor should use the most severe error mode", func(testConfig filterErrorModeTestConfig) {
			filters := make([]NamespacedFilter, 0, len(testConfig.errorModes))
			for _, errorMode := range testConfig.errorModes {
				filters = append(filters, NamespacedFilter{
					Namespace: namespace1,
					Filter: dash0common.Filter{
						ErrorMode: errorMode,
						Traces: &dash0common.TraceFilter{
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
				expected:   dash0common.FilterTransformErrorModeIgnore,
			}),
			Entry("single error mode is used", filterErrorModeTestConfig{
				errorModes: []dash0common.FilterTransformErrorMode{dash0common.FilterTransformErrorModeSilent},
				expected:   dash0common.FilterTransformErrorModeSilent,
			}),
			Entry("most severe error mode is used", filterErrorModeTestConfig{
				errorModes: []dash0common.FilterTransformErrorMode{
					dash0common.FilterTransformErrorModeSilent,
					dash0common.FilterTransformErrorModeIgnore,
					dash0common.FilterTransformErrorModePropagate,
				},
				expected: dash0common.FilterTransformErrorModePropagate,
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
								Transform: dash0common.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
									Traces: []dash0common.NormalizedTransformGroup{
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
								Transform: dash0common.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
									Metrics: []dash0common.NormalizedTransformGroup{
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
								Transform: dash0common.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
									Logs: []dash0common.NormalizedTransformGroup{
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
								Transform: dash0common.NormalizedTransformSpec{
									ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
									Traces: []dash0common.NormalizedTransformGroup{
										{Statements: []string{"trace statement 1"}},
										{Statements: []string{"trace statement 2"}},
									},
									Metrics: []dash0common.NormalizedTransformGroup{
										{Statements: []string{"metric statement 1"}},
										{Statements: []string{"metric statement 2"}},
									},
									Logs: []dash0common.NormalizedTransformGroup{
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
					Exporters:         cmTestSingleDefaultOtlpExporter(),
					KubernetesInfrastructureMetricsCollectionEnabled: true,
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
				transformProcessorRaw := ReadFromMap(
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
				groupsRaw := ReadFromMap(transformProcessor, []string{signalGroupsLabel})
				Expect(groupsRaw).ToNot(BeNil(),
					"expected %d transform group(s) but there were none for signal \"%s\"",
					len(expectedGroups), signal)
				groups := groupsRaw.([]interface{})
				Expect(groups).To(HaveLen(len(expectedGroups)))

				for i, expectedGroup := range expectedGroups {
					expectedStatements := expectedGroup.statements
					actualStatementsRaw := ReadFromMap(groups[i], []string{"statements"})
					Expect(actualStatementsRaw).ToNot(BeNil(),
						"expected %d transform statement(s) but there were none for signal \"%s\"",
						len(expectedStatements), signal)
					actualTransformStatements := actualStatementsRaw.([]interface{})
					Expect(actualTransformStatements).To(HaveLen(len(expectedStatements)))
					for i, expectedStatement := range expectedStatements {
						Expect(actualTransformStatements[i]).To(Equal(expectedStatement))
					}

					expectedConditions := expectedGroup.conditions
					actualConditionsRaw := ReadFromMap(groups[i], []string{"conditions"})
					Expect(actualConditionsRaw).ToNot(BeNil(),
						"expected %d transform condition(s) but there were none for signal \"%s\"",
						len(expectedConditions), signal)
					actualTransformConditions := actualConditionsRaw.([]interface{})
					Expect(actualTransformConditions).To(HaveLen(len(expectedConditions)))
					for i, expectedCondition := range expectedConditions {
						Expect(actualTransformConditions[i]).To(Equal(expectedCondition))
					}
				}

				commonProcessorsPipelineProcessors := readPipelineProcessors(pipelines, fmt.Sprintf("%s/common-processors", signal))
				Expect(commonProcessorsPipelineProcessors).To(ContainElements(transformProcessorName))
			}

			for _, signal := range expectations.signalsWithoutTransforms {
				transformProcessorName := fmt.Sprintf("transform/%s/custom_telemetry_transform", signal)
				transformProcessorRaw := ReadFromMap(
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
			errorModes []dash0common.FilterTransformErrorMode
			expected   dash0common.FilterTransformErrorMode
		}

		DescribeTable("transform processor should use the most severe error mode", func(testConfig transformErrorModeTestConfig) {
			transforms := make([]NamespacedTransform, 0, len(testConfig.errorModes))
			for _, errorMode := range testConfig.errorModes {
				transforms = append(transforms, NamespacedTransform{
					Namespace: namespace1,
					Transform: dash0common.NormalizedTransformSpec{
						ErrorMode: ptr.To(errorMode),
						Traces: []dash0common.NormalizedTransformGroup{
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
				expected:   dash0common.FilterTransformErrorModeIgnore,
			}),
			Entry("single error mode is used", transformErrorModeTestConfig{
				errorModes: []dash0common.FilterTransformErrorMode{dash0common.FilterTransformErrorModeSilent},
				expected:   dash0common.FilterTransformErrorModeSilent,
			}),
			Entry("most severe error mode is used", transformErrorModeTestConfig{
				errorModes: []dash0common.FilterTransformErrorMode{
					dash0common.FilterTransformErrorModeSilent,
					dash0common.FilterTransformErrorModeIgnore,
					dash0common.FilterTransformErrorModePropagate,
				},
				expected: dash0common.FilterTransformErrorModePropagate,
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
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				IsIPv6Cluster:     testConfig.ipv6,
			}

			expected := testConfig.expected
			configMap, err := assembleDaemonSetCollectorConfigMap(config, nil, nil, nil, nil, nil, emptyTargetAllocatorMtlsConfig, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			healthCheckEndpoint := ReadFromMap(collectorConfig, []string{"extensions", "health_check", "endpoint"})
			grpcOtlpEndpoint := ReadFromMap(collectorConfig, []string{"receivers", "otlp", "protocols", "grpc", "endpoint"})
			httpOtlpEndpoint := ReadFromMap(collectorConfig, []string{"receivers", "otlp", "protocols", "http", "endpoint"})
			Expect(healthCheckEndpoint).To(Equal(fmt.Sprintf("%s:13133", expected)))
			Expect(grpcOtlpEndpoint).To(Equal(fmt.Sprintf("%s:4317", expected)))
			Expect(httpOtlpEndpoint).To(Equal(fmt.Sprintf("%s:4318", expected)))

			configMap, err = assembleDeploymentCollectorConfigMap(config, nil, nil, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig = parseConfigMapContent(configMap)
			healthCheckEndpoint = ReadFromMap(collectorConfig, []string{"extensions", "health_check", "endpoint"})
			Expect(healthCheckEndpoint).To(Equal(fmt.Sprintf("%s:13133", expected)))
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

	Describe("render self monitoring pipeline", func() {

		DescribeTable("should not render the internal telemetry section if self monitoring is not enabled", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
					SelfMonitoringEnabled: false,
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readSelfMonitoringTelemetry(collectorConfig)).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should not render the internal telemetry section if there is no self monitoring export", func(cmTypeDef configMapTypeDefinition) {
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
				SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
					SelfMonitoringEnabled: true,
					Export:                dash0common.Export{},
				},
			}, monitoredNamespaces, nil, nil, false)
			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			Expect(readSelfMonitoringTelemetry(collectorConfig)).To(BeNil())
		}, daemonSetAndDeployment)

		DescribeTable("should render metrics & logs pipelines for a Dash0 export", func(cmTypeDef configMapTypeDefinition) {
			export := *Dash0ExportWithEndpointAndToken()
			configMap, err := cmTypeDef.assembleConfigMapFunction(&oTelColConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        namePrefix,
				Exporters:         cmTestSingleDefaultOtlpExporter(),
				KubernetesInfrastructureMetricsCollectionEnabled: true,
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

			selfMonitoringMetricsPipelineRaw := readSelfMonitoringMetricsPipeline(collectorConfig)
			Expect(selfMonitoringMetricsPipelineRaw).ToNot(BeNil())
			selfMonitoringMetricsPipeline, ok := selfMonitoringMetricsPipelineRaw.(map[string]interface{})
			Expect(ok).To(BeTrue())
			otlpExporter = readOtlpExporterFromSelfMonitoringMetricsPipeline(selfMonitoringMetricsPipeline)
			Expect(otlpExporter["protocol"]).To(Equal(common.ProtocolGrpc))
			Expect(otlpExporter["endpoint"]).To(Equal(EndpointDash0WithProtocolTest))
			headersRaw = otlpExporter["headers"]
			Expect(headersRaw).ToNot(BeNil())
			headers, ok = headersRaw.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(headers).To(HaveLen(1))
			Expect(headers[util.AuthorizationHeaderName]).To(Equal("Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"))
		}, daemonSetAndDeployment)
	})

	Describe("target-allocator config", func() {
		taMtlsConfig := TargetAllocatorMtlsConfig{
			Enabled:              true,
			ClientCertSecretName: "ta-mtls-client-cert-secret",
		}

		nsWithPrometheusScraping := []string{"ns1", "ns2", "ns3"}

		It("should not render the target-allocator config if prometheusCrdSupport is not enabled", func() {
			config := &oTelColConfig{
				OperatorNamespace:           OperatorNamespace,
				NamePrefix:                  namePrefix,
				Exporters:                   cmTestSingleDefaultOtlpExporter(),
				TargetAllocatorNamePrefix:   TargetAllocatorPrefixTest,
				PrometheusCrdSupportEnabled: false,
			}
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, nsWithPrometheusScraping, nil, nil, taMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			taConfig := ReadFromMap(collectorConfig, []string{"receivers", "prometheus", "target_allocator"})
			Expect(taConfig).To(BeNil())
		})

		It("should use the target-allocator HTTPS endpoint and configure TLS if mTLS is enabled", func() {
			config := &oTelColConfig{
				OperatorNamespace:           OperatorNamespace,
				NamePrefix:                  namePrefix,
				Exporters:                   cmTestSingleDefaultOtlpExporter(),
				TargetAllocatorNamePrefix:   TargetAllocatorPrefixTest,
				PrometheusCrdSupportEnabled: true,
			}
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, nsWithPrometheusScraping, nil, nil, taMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			taConfig := ReadFromMap(collectorConfig, []string{"receivers", "prometheus", "target_allocator"})
			Expect(taConfig).ToNot(BeNil())

			endpoint, ok := ReadFromMap(taConfig, []string{"endpoint"}).(string)
			Expect(ok).To(BeTrue())
			Expect(endpoint).To(HavePrefix("https://"))

			tlsConfig, ok := ReadFromMap(taConfig, []string{"tls"}).(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(tlsConfig["ca_file"]).To(Equal(fmt.Sprintf("%s/ca.crt", targetAllocatorCertsVolumeDir)))
			Expect(tlsConfig["cert_file"]).To(Equal(fmt.Sprintf("%s/tls.crt", targetAllocatorCertsVolumeDir)))
			Expect(tlsConfig["key_file"]).To(Equal(fmt.Sprintf("%s/tls.key", targetAllocatorCertsVolumeDir)))
		})

		It("should use the target-allocator HTTP endpoint if mTLS is disabled", func() {
			config := &oTelColConfig{
				OperatorNamespace:           OperatorNamespace,
				NamePrefix:                  namePrefix,
				Exporters:                   cmTestSingleDefaultOtlpExporter(),
				TargetAllocatorNamePrefix:   TargetAllocatorPrefixTest,
				PrometheusCrdSupportEnabled: true,
			}
			configMap, err := assembleDaemonSetCollectorConfigMap(config, []string{}, []string{}, nsWithPrometheusScraping, nil, nil, emptyTargetAllocatorMtlsConfig, false)

			Expect(err).ToNot(HaveOccurred())
			collectorConfig := parseConfigMapContent(configMap)
			taConfig := ReadFromMap(collectorConfig, []string{"receivers", "prometheus", "target_allocator"})
			Expect(taConfig).ToNot(BeNil())

			endpoint, ok := ReadFromMap(taConfig, []string{"endpoint"}).(string)
			Expect(ok).To(BeTrue())
			Expect(endpoint).To(HavePrefix("http://"))

			tlsConfig := ReadFromMap(taConfig, []string{"tls"})
			Expect(tlsConfig).To(BeNil())
		})
	})
})

func assembleDaemonSetCollectorConfigMapForTest(
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
		emptyTargetAllocatorMtlsConfig,
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
		nil,
		filters,
		transforms,
		forDeletion,
	)
}

func parseConfigMapContent(configMap *corev1.ConfigMap) map[string]interface{} {
	return ParseConfigMapContent(configMap, "config.yaml")
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
		ReadFromMap(
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

func readSelfMonitoringTelemetry(collectorConfig map[string]interface{}) interface{} {
	return ReadFromMap(
		collectorConfig,
		[]string{
			"service",
			"telemetry",
		})
}

func readSelfMonitoringMetricsPipeline(collectorConfig map[string]interface{}) interface{} {
	return ReadFromMap(
		collectorConfig,
		[]string{
			"service",
			"telemetry",
			"metrics",
		})
}

func readOtlpExporterFromSelfMonitoringMetricsPipeline(selfMonitoringLogsPipeline map[string]interface{}) map[string]interface{} {
	otlpExporterRaw := ReadFromMap(
		selfMonitoringLogsPipeline,
		[]string{
			"readers",
			"0",
			"periodic",
			"exporter",
			"otlp",
		},
	)
	Expect(otlpExporterRaw).ToNot(BeNil())
	otlpExporter, ok := otlpExporterRaw.(map[string]interface{})
	Expect(ok).To(BeTrue())
	return otlpExporter
}

func readSelfMonitoringLogsPipeline(collectorConfig map[string]interface{}) interface{} {
	return ReadFromMap(
		collectorConfig,
		[]string{
			"service",
			"telemetry",
			"logs",
		})
}

func readOtlpExporterFromSelfMonitoringLogsPipeline(selfMonitoringLogsPipeline map[string]interface{}) map[string]interface{} {
	otlpExporterRaw := ReadFromMap(
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

	var filter1 dash0common.Filter
	var filter2 dash0common.Filter
	switch signalT {
	case signalTypeTraces:
		switch objectT {
		case objectTypeSpan:
			filter1 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanFilter: conditionsNamespace2,
				},
			}
		case objectTypeSpanEvent:
			filter1 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanEventFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanEventFilter: conditionsNamespace2,
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeMetrics:
		switch objectT {
		case objectTypeMetric:
			filter1 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Metrics: &dash0common.MetricFilter{
					MetricFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Metrics: &dash0common.MetricFilter{
					MetricFilter: conditionsNamespace2,
				},
			}
		case objectTypeDataPoint:
			filter1 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Metrics: &dash0common.MetricFilter{
					DataPointFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Metrics: &dash0common.MetricFilter{
					DataPointFilter: conditionsNamespace2,
				},
			}
		default:
			Fail(fmt.Sprintf("unsupported object type %s for signal type %s", objectT, signalT))
		}

	case signalTypeLogs:
		switch objectT {
		case objectTypeLogRecord:
			filter1 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Logs: &dash0common.LogFilter{
					LogRecordFilter: conditionsNamespace1,
				},
			}
			filter2 = dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Logs: &dash0common.LogFilter{
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

	var transform1 dash0common.NormalizedTransformSpec
	var transform2 dash0common.NormalizedTransformSpec
	switch signalT {
	case signalTypeTraces:
		transform1 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Traces: []dash0common.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Traces: []dash0common.NormalizedTransformGroup{
				{Statements: statementsNamespace2},
			},
		}

	case signalTypeMetrics:
		transform1 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Metrics: []dash0common.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Metrics: []dash0common.NormalizedTransformGroup{
				{Statements: statementsNamespace2},
			},
		}

	case signalTypeLogs:
		transform1 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Logs: []dash0common.NormalizedTransformGroup{
				{Statements: statementsNamespace1},
			},
		}
		transform2 = dash0common.NormalizedTransformSpec{
			ErrorMode: ptr.To(dash0common.FilterTransformErrorModeIgnore),
			Logs: []dash0common.NormalizedTransformGroup{
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

func verifyRoutingTableEntry(table []interface{}, namespace string, expectedPipeline string) {
	expectedCondition := fmt.Sprintf(`attributes["k8s.namespace.name"] == "%s"`, namespace)
	found := false
	for _, entryRaw := range table {
		entry := entryRaw.(map[string]interface{})
		condition := entry["condition"].(string)
		if condition == expectedCondition {
			found = true
			Expect(entry["context"]).To(Equal("resource"))
			pipelines := entry["pipelines"].([]interface{})
			Expect(pipelines).To(ContainElement(expectedPipeline))
			break
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf("expected routing table entry for namespace %s not found", namespace))
}
