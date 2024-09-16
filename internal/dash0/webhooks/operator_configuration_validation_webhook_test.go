// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The validation webhook for the operator configuration resource", func() {

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
	})

	Describe("when validating", Ordered, func() {

		It("should reject operator configuration resources without export if self-monitoring is enabled", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: true,
						},
					},
				})
			Expect(err).To(MatchError(ContainSubstring(
				"admission webhook \"validate-operator-configuration.dash0.com\" denied the request: The provided " +
					"Dash0 operator configuration resource has self-monitoring enabled, but it does not have an " +
					"export configuration. Either disable self-monitoring or provide an export configuration for " +
					"self-monitoring telemetry.")))
		})

		It("should allow an operator configuration resource without export if self-monitoring is disabled", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: false,
						},
					},
				})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow an operator configuration resource with self-monitoring enabled if it has an export", func() {
			_, err := CreateOperatorConfigurationResource(
				ctx,
				k8sClient,
				&dash0v1alpha1.Dash0OperatorConfiguration{
					ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
					Spec:       OperatorConfigurationResourceDefaultSpec,
				})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
