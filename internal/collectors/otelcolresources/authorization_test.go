// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

//nolint:goconst
var _ = Describe(
	"Authorization", func() {

		Describe(
			"dash0ExporterAuthorizationForExport", func() {

				Context(
					"default export", func() {
						It(
							"should return authorization with default env var name for index 0", func() {
								export := *Dash0ExportWithEndpointAndToken()

								auth, err := dash0ExporterAuthorizationForExport(export, 0, true, nil)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth).NotTo(BeNil())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_DEFAULT_0"))
								Expect(auth.Authorization.Token).NotTo(BeNil())
								Expect(*auth.Authorization.Token).To(Equal(AuthorizationTokenTest))
							},
						)

						It(
							"should include the index in the env var name", func() {
								export := *Dash0ExportWithEndpointAndToken()

								auth, err := dash0ExporterAuthorizationForExport(export, 3, true, nil)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_DEFAULT_3"))
							},
						)

						It(
							"should copy authorization with secret ref", func() {
								export := *Dash0ExportWithEndpointAndSecretRef()

								auth, err := dash0ExporterAuthorizationForExport(export, 0, true, nil)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth.Authorization.Token).To(BeNil())
								Expect(auth.Authorization.SecretRef).NotTo(BeNil())
								Expect(auth.Authorization.SecretRef.Name).To(Equal(SecretRefTest.Name))
								Expect(auth.Authorization.SecretRef.Key).To(Equal(SecretRefTest.Key))
							},
						)

						It(
							"should ignore namespace when isDefault is true", func() {
								export := *Dash0ExportWithEndpointAndToken()
								ns := "some-namespace"

								auth, err := dash0ExporterAuthorizationForExport(export, 0, true, &ns)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_DEFAULT_0"))
							},
						)
					},
				)

				Context(
					"namespaced export", func() {
						It(
							"should return authorization with namespaced env var name", func() {
								export := *Dash0ExportWithEndpointAndToken()
								ns := "test-namespace"

								auth, err := dash0ExporterAuthorizationForExport(export, 0, false, &ns)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth).NotTo(BeNil())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_TEST_NAMESPACE_0"))
								Expect(auth.Authorization.Token).NotTo(BeNil())
								Expect(*auth.Authorization.Token).To(Equal(AuthorizationTokenTest))
							},
						)

						It(
							"should include the index in the namespaced env var name", func() {
								export := *Dash0ExportWithEndpointAndToken()
								ns := "test-namespace"

								auth, err := dash0ExporterAuthorizationForExport(export, 2, false, &ns)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_TEST_NAMESPACE_2"))
							},
						)

						It(
							"should replace hyphens with underscores and uppercase the namespace", func() {
								export := *Dash0ExportWithEndpointAndToken()
								ns := "my-complex-namespace"

								auth, err := dash0ExporterAuthorizationForExport(export, 0, false, &ns)

								Expect(err).NotTo(HaveOccurred())
								Expect(auth.EnvVarName).To(Equal("OTELCOL_AUTH_TOKEN_NS_MY_COMPLEX_NAMESPACE_0"))
							},
						)

						It(
							"should return error when namespace is nil", func() {
								export := *Dash0ExportWithEndpointAndToken()

								auth, err := dash0ExporterAuthorizationForExport(export, 0, false, nil)

								Expect(err).To(HaveOccurred())
								Expect(err.Error()).To(ContainSubstring("namespace"))
								Expect(auth).To(BeNil())
							},
						)

						It(
							"should copy authorization with alternative token", func() {
								alternativeToken := AuthorizationTokenTestAlternative
								export := dash0common.Export{
									Dash0: &dash0common.Dash0Configuration{
										Endpoint: EndpointDash0TestAlternative,
										Authorization: dash0common.Authorization{
											Token: &alternativeToken,
										},
									},
								}
								ns := "test-namespace"

								auth, err := dash0ExporterAuthorizationForExport(export, 0, false, &ns)

								Expect(err).NotTo(HaveOccurred())
								Expect(*auth.Authorization.Token).To(Equal(AuthorizationTokenTestAlternative))
							},
						)
					},
				)
			},
		)
	},
)
