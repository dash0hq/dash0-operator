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

//nolint:goconst
var _ = Describe("Exporter Conversion", func() {

	Describe("ConvertDash0ExporterToOtlpExporter", func() {
		It("should convert Dash0 config to OTLP exporter", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Dataset:  util.DatasetDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			auth := &dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", auth)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter).NotTo(BeNil())
			Expect(exporter.Name).To(Equal("otlp_grpc/dash0/default"))
			Expect(exporter.Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporter.Headers).To(HaveLen(1))
		})

		It("should return error when endpoint is empty", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: "",
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			auth := &dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", auth)

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
			auth := &dash0ExporterAuthorization{
				EnvVarName: nsEnvVar,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "ns/my-namespace", auth)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporter.Name).To(Equal("otlp_grpc/dash0/ns/my-namespace"))
			Expect(exporter.Headers[0].Value).To(Equal("Bearer ${env:OTELCOL_AUTH_TOKEN_NS_MY_NAMESPACE}"))
		})

		It("should set insecure for http:// endpoint", func() {
			d0Config := &dash0common.Dash0Configuration{
				Endpoint: "http://insecure-endpoint:4317",
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			auth := &dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}

			exporter, err := convertDash0ExporterToOtlpExporter(d0Config, "default", auth)

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
			Expect(exporter.Name).To(Equal("otlp_grpc/default"))
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
			Expect(exporter.Name).To(Equal("otlp_grpc/ns/test-namespace"))
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
			Expect(exporter.Name).To(Equal("otlp_http/default/proto"))
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
			Expect(exporter.Name).To(Equal("otlp_http/default/json"))
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
			Expect(exporter.Name).To(Equal("otlp_http/ns/test-namespace/proto"))
		})
	})

	Describe("ConvertExportSettingsToExporterList", func() {
		Context("with nil export", func() {
			It("should return nil when export is nil", func() {
				exporters, err := convertExportSettingsToExporterList(nil, 0, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(BeNil())
			})
		})

		Context("with default exporter", func() {
			It("should convert Dash0 export settings", func() {
				export := Dash0ExportWithEndpointAndToken()

				exporters, err := convertExportSettingsToExporterList(export, 0, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
			})

			It("should convert gRPC export settings", func() {
				export := GrpcExportTest()

				exporters, err := convertExportSettingsToExporterList(export, 0, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp_grpc/default_0"))
			})

			It("should convert HTTP export settings", func() {
				export := HttpExportTest()

				exporters, err := convertExportSettingsToExporterList(export, 0, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp_http/default_0/proto"))
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

				exporters, err := convertExportSettingsToExporterList(export, 0, true, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(3))
			})

			It("should return error when no exporter is configured", func() {
				export := &dash0common.Export{}

				exporters, err := convertExportSettingsToExporterList(export, 0, true, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no exporter configuration found"))
				Expect(exporters).To(BeNil())
			})
		})

		Context("with namespaced exporter", func() {
			It("should return error when namespace is nil for non-default", func() {
				export := Dash0ExportWithEndpointAndToken()

				exporters, err := convertExportSettingsToExporterList(export, 0, false, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no valid nameSuffix provided"))
				Expect(exporters).To(BeNil())
			})

			It("should use namespace suffix for namespaced exporter", func() {
				export := Dash0ExportWithEndpointAndToken()
				ns := "test-namespace"

				exporters, err := convertExportSettingsToExporterList(export, 0, false, &ns)

				Expect(err).NotTo(HaveOccurred())
				Expect(exporters).To(HaveLen(1))
				Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/ns/test-namespace_0"))
			})
		})
	})

	Describe("GetDefaultOtlpExporters", func() {
		It("should get default exporters from operator config", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: Dash0ExportWithEndpointAndToken().ToExports(),
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(1))
			Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
		})

		It("should return error when no export configured", func() {
			export := dash0common.Export{}
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: export.ToExports(),
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).To(HaveOccurred())
			Expect(exporters).To(BeNil())
		})

		It("should get exporters from multiple Exports entries", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{
						*Dash0ExportWithEndpointAndToken(),
						*GrpcExportTest(),
					},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(2))
			Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
			Expect(exporters[1].Name).To(Equal("otlp_grpc/default_1"))
		})

		It("should get exporters from multiple Exports entries with combined export types", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
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
						},
						*HttpExportTest(),
					},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(3))
			Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
			Expect(exporters[1].Name).To(Equal("otlp_grpc/default_0"))
			Expect(exporters[2].Name).To(Equal("otlp_http/default_1/proto"))
		})

		It("should return nil when operator config has no exports configured", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(BeNil())
		})

		It("should return error when one of multiple Exports entries has an error", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{
						*Dash0ExportWithEndpointAndToken(),
						{}, // empty export, will cause an error
					},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no exporter configuration found"))
			Expect(exporters).To(BeNil())
		})

		It("should get exporters from three Exports entries each with a different type", func() {
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Exports: []dash0common.Export{
						*Dash0ExportWithEndpointAndToken(),
						*GrpcExportTest(),
						*HttpExportTest(),
					},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(3))
			Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
			Expect(exporters[0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters[1].Name).To(Equal("otlp_grpc/default_1"))
			Expect(exporters[1].Endpoint).To(Equal(EndpointGrpcTest))
			Expect(exporters[2].Name).To(Equal("otlp_http/default_2/proto"))
			Expect(exporters[2].Endpoint).To(Equal(EndpointHttpTest))
		})

		It("should handle two Dash0 exports (different endpoints) in separate Exports entries", func() {
			alternativeToken := AuthorizationTokenTestAlternative
			operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
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
									Token: &alternativeToken,
								},
							},
						},
					},
				},
			}

			exporters, err := getDefaultOtlpExporters(operatorConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(exporters).To(HaveLen(2))
			Expect(exporters[0].Name).To(Equal("otlp_grpc/dash0/default_0"))
			Expect(exporters[0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters[1].Name).To(Equal("otlp_grpc/dash0/default_1"))
			Expect(exporters[1].Endpoint).To(Equal(EndpointDash0TestAlternative))
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
						Exports: nil,
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
						Exports: Dash0ExportWithEndpointAndToken().ToExports(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-2",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: GrpcExportTest().ToExports(),
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(2))
			Expect(exporters).To(HaveKey("namespace-1"))
			Expect(exporters).To(HaveKey("namespace-2"))
			Expect(exporters["namespace-1"][0].Name).To(Equal("otlp_grpc/dash0/ns/namespace-1_0"))
			Expect(exporters["namespace-2"][0].Name).To(Equal("otlp_grpc/ns/namespace-2_0"))
		})

		It("should skip monitoring resources with nil export", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-with-export",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: Dash0ExportWithEndpointAndToken().ToExports(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-without-export",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: nil,
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
						Exports: Dash0ExportWithEndpointAndToken().ToExports(),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "invalid-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: nil, // empty export causes error
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("valid-namespace"))
			Expect(exporters).NotTo(HaveKey("invalid-namespace"))
		})

		It("should return multiple exporters for a namespace with multiple Exports entries", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "multi-export-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*Dash0ExportWithEndpointAndToken(),
							*GrpcExportTest(),
						},
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("multi-export-namespace"))
			Expect(exporters["multi-export-namespace"]).To(HaveLen(2))
			Expect(exporters["multi-export-namespace"][0].Name).To(Equal("otlp_grpc/dash0/ns/multi-export-namespace_0"))
			Expect(exporters["multi-export-namespace"][0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters["multi-export-namespace"][1].Name).To(Equal("otlp_grpc/ns/multi-export-namespace_1"))
			Expect(exporters["multi-export-namespace"][1].Endpoint).To(Equal(EndpointGrpcTest))
		})

		It("should return exporters from multiple Exports entries with combined export types per namespace", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-with-combined",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
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
							},
							*HttpExportTest(),
						},
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("namespace-with-combined"))
			Expect(exporters["namespace-with-combined"]).To(HaveLen(3))
			Expect(exporters["namespace-with-combined"][0].Name).To(Equal("otlp_grpc/dash0/ns/namespace-with-combined_0"))
			Expect(exporters["namespace-with-combined"][0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters["namespace-with-combined"][1].Name).To(Equal("otlp_grpc/ns/namespace-with-combined_0"))
			Expect(exporters["namespace-with-combined"][1].Endpoint).To(Equal(EndpointGrpcTest))
			Expect(exporters["namespace-with-combined"][2].Name).To(Equal("otlp_http/ns/namespace-with-combined_1/proto"))
			Expect(exporters["namespace-with-combined"][2].Endpoint).To(Equal(EndpointHttpTest))
		})

		It("should handle multiple namespaces each with multiple Exports entries", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-1",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*Dash0ExportWithEndpointAndToken(),
							*GrpcExportTest(),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-2",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*HttpExportTest(),
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
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(2))

			// namespace-1: Dash0 + gRPC
			Expect(exporters).To(HaveKey("namespace-1"))
			Expect(exporters["namespace-1"]).To(HaveLen(2))
			Expect(exporters["namespace-1"][0].Name).To(Equal("otlp_grpc/dash0/ns/namespace-1_0"))
			Expect(exporters["namespace-1"][0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters["namespace-1"][1].Name).To(Equal("otlp_grpc/ns/namespace-1_1"))
			Expect(exporters["namespace-1"][1].Endpoint).To(Equal(EndpointGrpcTest))

			// namespace-2: HTTP + alternative Dash0
			Expect(exporters).To(HaveKey("namespace-2"))
			Expect(exporters["namespace-2"]).To(HaveLen(2))
			Expect(exporters["namespace-2"][0].Name).To(Equal("otlp_http/ns/namespace-2_0/proto"))
			Expect(exporters["namespace-2"][0].Endpoint).To(Equal(EndpointHttpTest))
			Expect(exporters["namespace-2"][1].Name).To(Equal("otlp_grpc/dash0/ns/namespace-2_1"))
			Expect(exporters["namespace-2"][1].Endpoint).To(Equal(EndpointDash0TestAlternative))
		})

		It("should skip namespace with error in one Exports entry and continue with other namespaces", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "valid-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*Dash0ExportWithEndpointAndToken(),
							*GrpcExportTest(),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "namespace-with-invalid-entry",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*Dash0ExportWithEndpointAndToken(),
							{}, // empty export will cause an error
						},
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			// The valid namespace should still have its exporters
			Expect(exporters).To(HaveKey("valid-namespace"))
			Expect(exporters["valid-namespace"]).To(HaveLen(2))

			// The namespace with the invalid entry should still have all exporters from the _valid_ entries
			// processed before the error was encountered. The implementation breaks on the first error
			// for a namespace, so only the first entry's exporters are kept.
			Expect(exporters).To(HaveKey("namespace-with-invalid-entry"))
			Expect(exporters["namespace-with-invalid-entry"]).To(HaveLen(1))
			Expect(exporters["namespace-with-invalid-entry"][0].Name).To(Equal("otlp_grpc/dash0/ns/namespace-with-invalid-entry_0"))
		})

		It("should return three Exports entries each with a different type for a single namespace", func() {
			monitoringResources := []dash0v1beta1.Dash0Monitoring{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MonitoringResourceName,
						Namespace: "triple-export-namespace",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						Exports: []dash0common.Export{
							*Dash0ExportWithEndpointAndToken(),
							*GrpcExportTest(),
							*HttpExportTest(),
						},
					},
				},
			}

			exporters := getNamespacedOtlpExporters(monitoringResources, &logger)

			Expect(exporters).To(HaveLen(1))
			Expect(exporters).To(HaveKey("triple-export-namespace"))
			Expect(exporters["triple-export-namespace"]).To(HaveLen(3))
			Expect(exporters["triple-export-namespace"][0].Name).To(Equal("otlp_grpc/dash0/ns/triple-export-namespace_0"))
			Expect(exporters["triple-export-namespace"][0].Endpoint).To(Equal(EndpointDash0Test))
			Expect(exporters["triple-export-namespace"][1].Name).To(Equal("otlp_grpc/ns/triple-export-namespace_1"))
			Expect(exporters["triple-export-namespace"][1].Endpoint).To(Equal(EndpointGrpcTest))
			Expect(exporters["triple-export-namespace"][2].Name).To(Equal("otlp_http/ns/triple-export-namespace_2/proto"))
			Expect(exporters["triple-export-namespace"][2].Endpoint).To(Equal(EndpointHttpTest))
		})
	})
})
