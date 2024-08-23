// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The OpenTelemetry Collector ConfigMap conent", func() {

	It("should fail if no exporter is configured", func() {
		_, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     dash0v1alpha1.Export{},
		})
		Expect(err).To(HaveOccurred())
	})

	It("should fail to render the Dash0 exporter when no endpoint is provided", func() {
		_, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		})
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")))

	})

	It("should render the Dash0 exporter", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointTest,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlp"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointTest))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Authorization"]).To(Equal("Bearer ${env:AUTH_TOKEN}"))
		Expect(headers["X-Dash0-Dataset"]).To(BeNil())
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		verifyPipelines(collectorConfig, "otlp")
	})

	It("should render the Dash0 exporter with custom dataset", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointTest,
					Dataset:  "custom-dataset",
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlp"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointTest))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers["Authorization"]).To(Equal("Bearer ${env:AUTH_TOKEN}"))
		Expect(headers["X-Dash0-Dataset"]).To(Equal("custom-dataset"))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		verifyPipelines(collectorConfig, "otlp")
	})

	It("should fail to render a gRPC exporter when no endpoint is provided", func() {
		_, err := collectorConfigMap(&oTelColConfig{
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
		})
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")))

	})

	It("should render an arbitrary gRPC exporter", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: "example.com:4317",
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
		})

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlp"]
		Expect(exporter2).ToNot(BeNil())
		otlpGrpcExporter := exporter2.(map[string]interface{})
		Expect(otlpGrpcExporter).ToNot(BeNil())
		Expect(otlpGrpcExporter["endpoint"]).To(Equal("example.com:4317"))
		headersRaw := otlpGrpcExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(otlpGrpcExporter["encoding"]).To(BeNil())

		verifyPipelines(collectorConfig, "otlp")
	})

	It("should fail to render an HTTP exporter when no endpoint is provided", func() {
		_, err := collectorConfigMap(&oTelColConfig{
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
		})
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")))
	})

	It("should fail to render an HTTP exporter when no encoding is provided", func() {
		_, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: "https://example.com:4318",
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
			},
		})
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")))

	})

	It("should render an arbitrary HTTP exporter", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: "https://example.com:4318",
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
					Encoding: "json",
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(2))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlphttp"]
		Expect(exporter2).ToNot(BeNil())
		otlpHttpExporter := exporter2.(map[string]interface{})
		Expect(otlpHttpExporter).ToNot(BeNil())
		Expect(otlpHttpExporter["endpoint"]).To(Equal("https://example.com:4318"))
		headersRaw := otlpHttpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(2))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(otlpHttpExporter["encoding"]).To(Equal("json"))

		verifyPipelines(collectorConfig, "otlphttp")
	})

	It("should refuse to render the Dash0 exporter together with a gRPC exporter", func() {
		_, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointTest,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: "https://example.com:4318",
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
			},
		})
		Expect(err).To(
			MatchError(
				ContainSubstring(
					"combining the Dash0 exporter with a gRPC exporter is not supported, please use only one of them")))
	})

	It("should render the Dash0 exporter together with an HTTP exporter", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointTest,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: "https://example.com:4318",
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
					Encoding: "proto",
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(3))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlp"]
		Expect(exporter2).ToNot(BeNil())
		dash0OtlpExporter := exporter2.(map[string]interface{})
		Expect(dash0OtlpExporter).ToNot(BeNil())
		Expect(dash0OtlpExporter["endpoint"]).To(Equal(EndpointTest))
		headersRaw := dash0OtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Authorization"]).To(Equal("Bearer ${env:AUTH_TOKEN}"))
		Expect(dash0OtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlphttp"]
		Expect(exporter3).ToNot(BeNil())
		httpExporter := exporter3.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal("https://example.com:4318"))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(httpExporter["encoding"]).To(Equal("proto"))

		verifyPipelines(collectorConfig, "otlp", "otlphttp")
	})

	It("should render a gRPC exporter together with an HTTP exporter", func() {
		configMap, err := collectorConfigMap(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Grpc: &dash0v1alpha1.GrpcConfiguration{
					Endpoint: "example.com:4317",
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key1",
						Value: "Value1",
					}},
				},
				Http: &dash0v1alpha1.HttpConfiguration{
					Endpoint: "https://example.com:4318",
					Headers: []dash0v1alpha1.Header{{
						Name:  "Key2",
						Value: "Value2",
					}},
					Encoding: "proto",
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		collectorConfig := parseConfigMapContent(configMap)
		exportersRaw := collectorConfig["exporters"]
		Expect(exportersRaw).ToNot(BeNil())
		exporters := exportersRaw.(map[string]interface{})
		Expect(exporters).To(HaveLen(3))
		debugExporter := exporters["debug"]
		Expect(debugExporter).ToNot(BeNil())

		exporter2 := exporters["otlp"]
		Expect(exporter2).ToNot(BeNil())
		grpcOtlpExporter := exporter2.(map[string]interface{})
		Expect(grpcOtlpExporter).ToNot(BeNil())
		Expect(grpcOtlpExporter["endpoint"]).To(Equal("example.com:4317"))
		headersRaw := grpcOtlpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers := headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key1"]).To(Equal("Value1"))
		Expect(grpcOtlpExporter["encoding"]).To(BeNil())

		exporter3 := exporters["otlphttp"]
		Expect(exporter3).ToNot(BeNil())
		httpExporter := exporter3.(map[string]interface{})
		Expect(httpExporter["endpoint"]).To(Equal("https://example.com:4318"))
		headersRaw = httpExporter["headers"]
		Expect(headersRaw).ToNot(BeNil())
		headers = headersRaw.(map[string]interface{})
		Expect(headers).To(HaveLen(1))
		Expect(headers).To(HaveLen(1))
		Expect(headers["Key2"]).To(Equal("Value2"))
		Expect(httpExporter["encoding"]).To(Equal("proto"))

		verifyPipelines(collectorConfig, "otlp", "otlphttp")
	})
})

func parseConfigMapContent(configMap *corev1.ConfigMap) map[string]interface{} {
	configMapContent := configMap.Data["config.yaml"]
	configMapParsed := &map[string]interface{}{}
	err := yaml.Unmarshal([]byte(configMapContent), configMapParsed)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Cannot parse config map content:\n%s\n", configMapContent))
	return *configMapParsed
}

func verifyPipelines(collectorConfig map[string]interface{}, expectedExporters ...string) {
	pipelines := ((collectorConfig["service"]).(map[string]interface{})["pipelines"]).(map[string]interface{})
	Expect(pipelines).ToNot(BeNil())
	tracesPipeline := (pipelines["traces/downstream"]).(map[string]interface{})
	tracesExporters := (tracesPipeline["exporters"]).([]interface{})
	Expect(tracesExporters).To(ContainElements(expectedExporters))
	metricsPipeline := (pipelines["metrics/downstream"]).(map[string]interface{})
	metricsExporters := (metricsPipeline["exporters"]).([]interface{})
	Expect(metricsExporters).To(ContainElements(expectedExporters))
	logsPipeline := (pipelines["logs/downstream"]).(map[string]interface{})
	logsExporters := (logsPipeline["exporters"]).([]interface{})
	Expect(logsExporters).To(ContainElements(expectedExporters))
}
