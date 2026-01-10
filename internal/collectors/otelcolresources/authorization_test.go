// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("Authorization", func() {

	Describe("authEnvVarNameForNs", func() {
		It("should convert a simple namespace to env var name", func() {
			result := authEnvVarNameForNs("default")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_DEFAULT"))
		})

		It("should convert a namespace with hyphens to env var name with underscores", func() {
			result := authEnvVarNameForNs("my-namespace")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_MY_NAMESPACE"))
		})

		It("should convert a namespace with multiple hyphens to env var name", func() {
			result := authEnvVarNameForNs("my-long-namespace-name")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_MY_LONG_NAMESPACE_NAME"))
		})

		It("should convert lowercase letters to uppercase", func() {
			result := authEnvVarNameForNs("myNamespace")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_MYNAMESPACE"))
		})

		It("should handle a namespace that is already uppercase", func() {
			result := authEnvVarNameForNs("UPPERCASE")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_UPPERCASE"))
		})

		It("should handle a namespace with numbers", func() {
			result := authEnvVarNameForNs("namespace-123")
			Expect(result).To(Equal("OTELCOL_AUTH_TOKEN_NS_NAMESPACE_123"))
		})
	})

	Describe("dash0ExporterAuthorizations.all", func() {
		It("should return an empty slice when there are no authorizations", func() {
			auths := dash0ExporterAuthorizations{
				DefaultDash0ExporterAuthorization:     nil,
				NamespacedDash0ExporterAuthorizations: nil,
			}
			result := auths.all()
			Expect(result).To(HaveLen(0))
		})

		It("should return only the default authorization when no namespaced authorizations exist", func() {
			defaultAuth := dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			auths := dash0ExporterAuthorizations{
				DefaultDash0ExporterAuthorization:     &defaultAuth,
				NamespacedDash0ExporterAuthorizations: nil,
			}
			result := auths.all()
			Expect(result).To(HaveLen(1))
			Expect(result).To(ContainElement(defaultAuth))
		})

		It("should return only namespaced authorizations when no default authorization exists", func() {
			nsAuth := dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameForNs("test-namespace"),
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			auths := dash0ExporterAuthorizations{
				DefaultDash0ExporterAuthorization: nil,
				NamespacedDash0ExporterAuthorizations: dash0ExporterAuthorizationByNamespace{
					"test-namespace": nsAuth,
				},
			}
			result := auths.all()
			Expect(result).To(HaveLen(1))
			Expect(result).To(ContainElement(nsAuth))
		})

		It("should return both default and namespaced authorizations", func() {
			defaultAuth := dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameDefault,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			nsAuth1 := dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameForNs("namespace-1"),
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			}
			nsAuth2 := dash0ExporterAuthorization{
				EnvVarName: authEnvVarNameForNs("namespace-2"),
				Authorization: dash0common.Authorization{
					SecretRef: &SecretRefTest,
				},
			}
			auths := dash0ExporterAuthorizations{
				DefaultDash0ExporterAuthorization: &defaultAuth,
				NamespacedDash0ExporterAuthorizations: dash0ExporterAuthorizationByNamespace{
					"namespace-1": nsAuth1,
					"namespace-2": nsAuth2,
				},
			}
			result := auths.all()
			Expect(result).To(HaveLen(3))
			Expect(result).To(ContainElement(defaultAuth))
			Expect(result).To(ContainElement(nsAuth1))
			Expect(result).To(ContainElement(nsAuth2))
		})
	})

	Describe("collectDash0ExporterAuthorizations", func() {
		var (
			operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration
		)

		BeforeEach(func() {
			operatorConfig = &dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-operator-config",
				},
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: &dash0common.Export{},
				},
			}
		})

		Context("when operator configuration has Dash0 export with token", func() {
			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				}
			})

			It("should set the default authorization from operator config", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, nil)

				Expect(result.DefaultDash0ExporterAuthorization).NotTo(BeNil())
				Expect(result.DefaultDash0ExporterAuthorization.EnvVarName).To(Equal(authEnvVarNameDefault))
				Expect(result.DefaultDash0ExporterAuthorization.Authorization.Token).To(Equal(&AuthorizationTokenTest))
			})

			It("should have empty namespaced authorizations when no monitoring resources exist", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, nil)

				Expect(result.NamespacedDash0ExporterAuthorizations).To(BeEmpty())
			})
		})

		Context("when operator configuration has Dash0 export with secret ref", func() {
			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							SecretRef: &SecretRefTest,
						},
					},
				}
			})

			It("should set the default authorization with secret ref", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, nil)

				Expect(result.DefaultDash0ExporterAuthorization).NotTo(BeNil())
				Expect(result.DefaultDash0ExporterAuthorization.EnvVarName).To(Equal(authEnvVarNameDefault))
				Expect(result.DefaultDash0ExporterAuthorization.Authorization.SecretRef).To(Equal(&SecretRefTest))
			})
		})

		Context("when operator configuration does not have Dash0 export", func() {
			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Grpc: &dash0common.GrpcConfiguration{
						Endpoint: EndpointGrpcTest,
					},
				}
			})

			It("should have nil default authorization", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, nil)

				Expect(result.DefaultDash0ExporterAuthorization).To(BeNil())
			})
		})

		Context("when monitoring resources have Dash0 export", func() {
			var monitoringResources []dash0v1beta1.Dash0Monitoring

			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				}
				monitoringResources = []dash0v1beta1.Dash0Monitoring{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "namespace-1",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: &dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0TestAlternative,
									Authorization: dash0common.Authorization{
										Token: &AuthorizationTokenTestAlternative,
									},
								},
							},
						},
					},
				}
			})

			It("should include namespaced authorization from monitoring resource", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, monitoringResources)

				Expect(result.NamespacedDash0ExporterAuthorizations).NotTo(BeNil())
				Expect(result.NamespacedDash0ExporterAuthorizations).To(HaveKey("namespace-1"))
				nsAuth := result.NamespacedDash0ExporterAuthorizations["namespace-1"]
				Expect(nsAuth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_NAMESPACE_1"))
			})
		})

		Context("when monitoring resources do not have Dash0 export", func() {
			var monitoringResources []dash0v1beta1.Dash0Monitoring

			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				}
				monitoringResources = []dash0v1beta1.Dash0Monitoring{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "namespace-without-dash0-export",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: &dash0common.Export{
								Grpc: &dash0common.GrpcConfiguration{
									Endpoint: EndpointGrpcTest,
								},
							},
						},
					},
				}
			})

			It("should not include namespaced authorization for monitoring resource without Dash0 export", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, monitoringResources)

				Expect(result.NamespacedDash0ExporterAuthorizations).To(BeEmpty())
			})
		})

		Context("with multiple monitoring resources", func() {
			var monitoringResources []dash0v1beta1.Dash0Monitoring

			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				}
				monitoringResources = []dash0v1beta1.Dash0Monitoring{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "namespace-with-dash0",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: &dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0TestAlternative,
									Authorization: dash0common.Authorization{
										Token: &AuthorizationTokenTestAlternative,
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "namespace-without-dash0",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: &dash0common.Export{
								Http: &dash0common.HttpConfiguration{
									Endpoint: EndpointHttpTest,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "another-namespace-with-dash0",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: &dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0Test,
									Authorization: dash0common.Authorization{
										SecretRef: &SecretRefTest,
									},
								},
							},
						},
					},
				}
			})

			It("should include authorizations only for monitoring resources with Dash0 export", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, monitoringResources)

				Expect(result.NamespacedDash0ExporterAuthorizations).NotTo(BeNil())
				Expect(result.NamespacedDash0ExporterAuthorizations).To(HaveLen(2))
				Expect(result.NamespacedDash0ExporterAuthorizations).To(HaveKey("namespace-with-dash0"))
				Expect(result.NamespacedDash0ExporterAuthorizations).To(HaveKey("another-namespace-with-dash0"))
				Expect(result.NamespacedDash0ExporterAuthorizations).NotTo(HaveKey("namespace-without-dash0"))
			})

			It("should generate correct env var names for each namespace", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, monitoringResources)

				ns1Auth := result.NamespacedDash0ExporterAuthorizations["namespace-with-dash0"]
				Expect(ns1Auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_NAMESPACE_WITH_DASH0"))

				ns2Auth := result.NamespacedDash0ExporterAuthorizations["another-namespace-with-dash0"]
				Expect(ns2Auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_ANOTHER_NAMESPACE_WITH_DASH0"))
			})
		})

		Context("when monitoring resource has nil export", func() {
			var monitoringResources []dash0v1beta1.Dash0Monitoring

			BeforeEach(func() {
				operatorConfig.Spec.Export = &dash0common.Export{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0common.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				}
				monitoringResources = []dash0v1beta1.Dash0Monitoring{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      MonitoringResourceName,
							Namespace: "namespace-with-nil-export",
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Export: nil,
						},
					},
				}
			})

			It("should not include namespaced authorization for monitoring resource with nil export", func() {
				result := collectDash0ExporterAuthorizations(operatorConfig, monitoringResources)

				Expect(result.NamespacedDash0ExporterAuthorizations).To(BeEmpty())
			})
		})
	})
})
