// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	EndpointDash0Test = "endpoint.dash0.com:4317"
	EndpointHttpTest  = "https://endpoint.backend.com:4318"
)

var (
	SecretRefTest = dash0common.SecretRef{
		Name: "secret-ref",
		Key:  "key",
	}
)

var _ = Describe("v1beta1 Dash0 monitoring CRD", func() {
	Describe("cloneAndRedact", func() {

		It("should clear ManagedFields", func() {
			original := Dash0Monitoring{}
			original.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "some-manager"}}

			redacted := original.cloneAndRedact()

			Expect(redacted.ManagedFields).To(BeNil())
			Expect(original.ManagedFields).To(HaveLen(1))
		})

		It("should handle a resource with no exports", func() {
			original := Dash0Monitoring{}
			redacted := original.cloneAndRedact()
			Expect(redacted.Spec.Exports).To(BeEmpty())
		})

		It("should redact the Dash0 token", func() {
			token := "my-secret-token"
			original := Dash0Monitoring{
				Spec: Dash0MonitoringSpec{
					Exports: []dash0common.Export{{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0common.Authorization{
								Token: &token,
							},
						},
					}},
				},
			}

			redacted := original.cloneAndRedact()

			Expect(*redacted.Spec.Exports[0].Dash0.Authorization.Token).To(Equal("<redacted>"))
			Expect(*original.Spec.Exports[0].Dash0.Authorization.Token).To(Equal("my-secret-token"))
		})

		It("should not redact a Dash0 export that uses a SecretRef instead of a token", func() {
			original := Dash0Monitoring{
				Spec: Dash0MonitoringSpec{
					Exports: []dash0common.Export{{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0common.Authorization{
								SecretRef: new(SecretRefTest),
							},
						},
					}},
				},
			}

			redacted := original.cloneAndRedact()

			Expect(redacted.Spec.Exports[0].Dash0.Authorization.Token).To(BeNil())
			Expect(redacted.Spec.Exports[0].Dash0.Authorization.SecretRef).ToNot(BeNil())
			Expect(redacted.Spec.Exports[0].Dash0.Authorization.SecretRef.Name).To(Equal(SecretRefTest.Name))
		})

		It("should redact HTTP header values", func() {
			original := Dash0Monitoring{
				Spec: Dash0MonitoringSpec{
					Exports: []dash0common.Export{{
						Http: &dash0common.HttpConfiguration{
							Endpoint: EndpointHttpTest,
							Headers: []dash0common.Header{
								{Name: "Authorization", Value: "Bearer secret-token"},
								{Name: "X-Custom-Header", Value: "another-secret"},
							},
						},
					}},
				},
			}

			redacted := original.cloneAndRedact()

			Expect(redacted.Spec.Exports[0].Http.Headers[0].Name).To(Equal("Authorization"))
			Expect(redacted.Spec.Exports[0].Http.Headers[0].Value).To(Equal("<redacted>"))
			Expect(redacted.Spec.Exports[0].Http.Headers[1].Name).To(Equal("X-Custom-Header"))
			Expect(redacted.Spec.Exports[0].Http.Headers[1].Value).To(Equal("<redacted>"))
			Expect(original.Spec.Exports[0].Http.Headers[0].Value).To(Equal("Bearer secret-token"))
			Expect(original.Spec.Exports[0].Http.Headers[1].Value).To(Equal("another-secret"))
		})

		It("should redact gRPC header values", func() {
			original := Dash0Monitoring{
				Spec: Dash0MonitoringSpec{
					Exports: []dash0common.Export{{
						Grpc: &dash0common.GrpcConfiguration{
							Endpoint: EndpointHttpTest,
							Headers: []dash0common.Header{
								{Name: "Authorization", Value: "Bearer secret-token"},
							},
						},
					}},
				},
			}

			redacted := original.cloneAndRedact()

			Expect(redacted.Spec.Exports[0].Grpc.Headers[0].Name).To(Equal("Authorization"))
			Expect(redacted.Spec.Exports[0].Grpc.Headers[0].Value).To(Equal("<redacted>"))
			Expect(original.Spec.Exports[0].Grpc.Headers[0].Value).To(Equal("Bearer secret-token"))
		})
	})
})
