// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Test helper functions (existing)
func DefaultOtlpExportersTest() []otlpExporter {
	return []otlpExporter{{
		Name:     "otlp/dash0/default",
		Endpoint: EndpointDash0Test,
		Headers: []dash0common.Header{
			{
				Name:  util.AuthorizationHeaderName,
				Value: AuthorizationTokenTest,
			},
		},
	},
	}
}

func DefaultOtlpExportersWithCustomDatasetTest() []otlpExporter {
	return []otlpExporter{{
		Name:     "otlp/dash0/default",
		Endpoint: EndpointDash0Test,
		Headers: []dash0common.Header{
			{
				Name:  util.AuthorizationHeaderName,
				Value: AuthorizationTokenTest,
			},
			{
				Name:  util.Dash0DatasetHeaderName,
				Value: DatasetCustomTest,
			},
		},
	},
	}
}

func DefaultOtlpExportersWithGrpc() []otlpExporter {
	return []otlpExporter{{
		Name:     "otlp/grpc/default",
		Endpoint: EndpointDash0Test,
		Headers: []dash0common.Header{
			{
				Name:  "Key",
				Value: "Value",
			},
		},
	},
	}
}

func DefaultOtlpExportersWithHttp() []otlpExporter {
	return []otlpExporter{{
		Name:     "otlp/grpc/default",
		Endpoint: EndpointDash0Test,
		Headers: []dash0common.Header{
			{
				Name:  "Key",
				Value: "Value",
			},
		},
	},
	}
}

var _ = Describe("Exporter", func() {

	Describe("namespaceToNameSuffix", func() {
		It("should convert namespace to name suffix", func() {
			Expect(namespaceToNameSuffix("default")).To(Equal("ns/default"))
		})

		It("should handle namespace with hyphens", func() {
			Expect(namespaceToNameSuffix("my-namespace")).To(Equal("ns/my-namespace"))
		})
	})

	Describe("authHeaderValue", func() {
		It("should generate correct auth header value for default env var", func() {
			result := authHeaderValue(authEnvVarNameDefault)
			Expect(result).To(Equal("Bearer ${env:OTELCOL_AUTH_TOKEN_DEFAULT}"))
		})

		It("should generate correct auth header value for namespaced env var", func() {
			envVarName := authEnvVarNameForNs("test-namespace")
			result := authHeaderValue(envVarName)
			Expect(result).To(Equal("Bearer ${env:OTELCOL_AUTH_TOKEN_NS_TEST_NAMESPACE}"))
		})
	})

	Describe("ConvertDash0ExporterToOtlpExporter", func() {
		It("should convert Dash0 config to OTLP exporter with default suffix", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", authEnvVarNameDefault)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter).NotTo(BeNil())
			Expect(exporter.Name).To(Equal("otlp/dash0/default"))
			Expect(exporter.Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporter.Headers).To(HaveLen(1))
			Expect(exporter.Headers[0].Name).To(Equal(util.AuthorizationHeaderName))
			Expect(exporter.Headers[0].Value).To(Equal("Bearer ${env:OTELCOL_AUTH_TOKEN_DEFAULT}"))
		})

		It("should convert Dash0 config with custom dataset", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Dataset:  DatasetCustomTest,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", authEnvVarNameDefault)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Headers).To(HaveLen(2))
			Expect(exporter.Headers[1].Name).To(Equal(util.Dash0DatasetHeaderName))
			Expect(exporter.Headers[1].Value).To(Equal(DatasetCustomTest))
		})

		It("should not add dataset header for default dataset", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Dataset:  util.DatasetDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", authEnvVarNameDefault)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Headers).To(HaveLen(1))
		})

		It("should return error when endpoint is empty", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: "",
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", authEnvVarNameDefault)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no endpoint provided"))
			Expect(exporter).To(BeNil())
		})

		It("should use namespace suffix for namespaced exporter", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			nsEnvVar := authEnvVarNameForNs("my-namespace")

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "ns/my-namespace", nsEnvVar)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Name).To(Equal("otlp/dash0/ns/my-namespace"))
			Expect(exporter.Headers[0].Value).To(Equal("Bearer ${env:OTELCOL_AUTH_TOKEN_NS_MY_NAMESPACE}"))
		})

		It("should set insecure for http:// endpoint", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: "http://insecure-endpoint:4317",
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", authEnvVarNameDefault)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Insecure).To(BeTrue())
		})
	})

	Describe("ConvertGrpcExporterToOtlpExporter", func() {
		It("should convert gRPC config to OTLP exporter", func() {
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint: EndpointGrpcTest,
				Headers: []dash0common.Header{
					{Name: "Key", Value: "Value"},
				},
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter).NotTo(BeNil())
			Expect(exporter.Name).To(Equal("otlp/grpc/default"))
			Expect(exporter.Endpoint).To(Equal(EndpointGrpcTest))
			Expect(exporter.Headers).To(HaveLen(1))
			Expect(exporter.Headers[0].Name).To(Equal("Key"))
		})

		It("should return error when endpoint is empty", func() {
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint: "",
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no endpoint provided for the gRPC exporter"))
			Expect(exporter).To(BeNil())
		})

		It("should respect explicit insecure setting", func() {
			insecure := true
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint: "https://secure-endpoint:4317",
				Insecure: &insecure,
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Insecure).To(BeTrue())
		})

		It("should set insecure for http:// endpoint when not explicitly set", func() {
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint: "http://insecure-endpoint:4317",
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Insecure).To(BeTrue())
		})

		It("should set insecureSkipVerify when specified for https endpoint", func() {
			skipVerify := true
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint:           "https://endpoint:4317",
				InsecureSkipVerify: &skipVerify,
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.InsecureSkipVerify).To(BeTrue())
		})

		It("should not set insecureSkipVerify for http endpoint even if specified", func() {
			skipVerify := true
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint:           "http://endpoint:4317",
				InsecureSkipVerify: &skipVerify,
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.InsecureSkipVerify).To(BeFalse())
		})

		It("should use namespace suffix for namespaced exporter", func() {
			grpcConfig := &dash0common.GrpcConfiguration{
				Endpoint: EndpointGrpcTest,
			}

			exporter, err := convertGrpcExporterToOtlpExporter(grpcConfig, "ns/test-namespace")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Name).To(Equal("otlp/grpc/ns/test-namespace"))
		})
	})

	Describe("ConvertHttpExporterToOtlpExporter", func() {
		It("should convert HTTP config with proto encoding to OTLP exporter", func() {
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint: EndpointHttpTest,
				Encoding: dash0common.Proto,
				Headers: []dash0common.Header{
					{Name: "Key", Value: "Value"},
				},
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter).NotTo(BeNil())
			Expect(exporter.Name).To(Equal("otlphttp/default/proto"))
			Expect(exporter.Endpoint).To(Equal(EndpointHttpTest))
			Expect(exporter.Encoding).To(Equal("proto"))
			Expect(exporter.Headers).To(HaveLen(1))
		})

		It("should convert HTTP config with json encoding to OTLP exporter", func() {
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint: EndpointHttpTest,
				Encoding: dash0common.Json,
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Name).To(Equal("otlphttp/default/json"))
			Expect(exporter.Encoding).To(Equal("json"))
		})

		It("should return error when endpoint is empty", func() {
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint: "",
				Encoding: dash0common.Proto,
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "default")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no endpoint provided for the HTTP exporter"))
			Expect(exporter).To(BeNil())
		})

		It("should return error when encoding is empty", func() {
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint: EndpointHttpTest,
				Encoding: "",
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "default")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no encoding provided for the HTTP exporter"))
			Expect(exporter).To(BeNil())
		})

		It("should set insecureSkipVerify when specified for https endpoint", func() {
			skipVerify := true
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint:           "https://endpoint:4318",
				Encoding:           dash0common.Proto,
				InsecureSkipVerify: &skipVerify,
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "default")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.InsecureSkipVerify).To(BeTrue())
		})

		It("should use namespace suffix for namespaced exporter", func() {
			httpConfig := &dash0common.HttpConfiguration{
				Endpoint: EndpointHttpTest,
				Encoding: dash0common.Proto,
			}

			exporter, err := convertHttpExporterToOtlpExporter(httpConfig, "ns/test-namespace")

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Name).To(Equal("otlphttp/ns/test-namespace/proto"))
		})
	})

	Describe("ConvertExportSettingsToExporterList", func() {
		Context("with nil export", func() {
			It("should return nil when export is nil", func() {
				exporters, err := convertExportSettingsToExporterList(nil, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(BeNil())
			})
		})

		Context("with default exporter", func() {
			It("should convert Dash0 export settings", func() {
				export := Dash0ExportWithEndpointAndToken()

				exporters, err := convertExportSettingsToExporterList(export, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp/dash0/default"))
			})

			It("should convert gRPC export settings", func() {
				export := GrpcExportTest()

				exporters, err := convertExportSettingsToExporterList(export, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp/grpc/default"))
			})

			It("should convert HTTP export settings", func() {
				export := HttpExportTest()

				exporters, err := convertExportSettingsToExporterList(export, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlphttp/default/proto"))
			})

			It("should convert combined export settings", func() {
				export := &dash0common.Export{
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
				}

				exporters, err := convertExportSettingsToExporterList(export, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(3))
			})

			It("should return error when no exporter is configured", func() {
				export := &dash0common.Export{}

				exporters, err := convertExportSettingsToExporterList(export, true, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no exporter configuration found"))
				Expect(exporters).To(BeNil())
			})
		})

		Context("with namespaced exporter", func() {
			It("should return error when namespace is nil for non-default", func() {
				export := Dash0ExportWithEndpointAndToken()

				exporters, err := convertExportSettingsToExporterList(export, false, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no valid nameSuffix provided"))
				Expect(exporters).To(BeNil())
			})

			It("should use namespace suffix for namespaced exporter", func() {
				export := Dash0ExportWithEndpointAndToken()
				ns := "test-namespace"

				exporters, err := convertExportSettingsToExporterList(export, false, &ns)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp/dash0/ns/test-namespace"))
			})
		})
	})

	Describe("GetDefaultOtlpExporters", func() {
		It("should get default exporters from operator config", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(1))
			Expect(exporters[0].Name).To(Equal("otlp/dash0/default"))
		})

		It("should return error when no export configured", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: &dash0common.Export{},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).To(HaveOccurred())
			Expect(exporters).To(BeNil())
		})
	})

	Describe("GetNamespacedOtlpExporters", func() {
		var logger logr.Logger

		BeforeEach(func() {
			logger = logr.Discard()
		})

		It("should return empty map when no monitoring resources have export", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "test-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: nil,
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(BeEmpty())
		})

		It("should return exporters for monitoring resources with export", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-1",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: Dash0ExportWithEndpointAndToken(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-2",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: GrpcExportTest(),
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(2))
			Expect(exporters).To(HaveKey("namespace-1"))
			Expect(exporters).To(HaveKey("namespace-2"))
			Expect(exporters["namespace-1"][0].Name).To(Equal("otlp/dash0/ns/namespace-1"))
			Expect(exporters["namespace-2"][0].Name).To(Equal("otlp/grpc/ns/namespace-2"))
		})

		It("should skip monitoring resources with nil export", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-with-export",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: Dash0ExportWithEndpointAndToken(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-without-export",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: nil,
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("namespace-with-export"))
			Expect(exporters).NotTo(HaveKey("namespace-without-export"))
		})

		It("should continue on error and skip invalid export", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "valid-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: Dash0ExportWithEndpointAndToken(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "invalid-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Export: &dash0common.Export{}, // empty export causes error
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("valid-namespace"))
			Expect(exporters).NotTo(HaveKey("invalid-namespace"))
		})
	})
})
