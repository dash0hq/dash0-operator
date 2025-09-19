// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postinstall

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testTimeout     = 2 * time.Second
	pollingInterval = 100 * time.Millisecond
)

var _ = Describe("Waiting for the Dash0 operator configuration auto resource", Ordered, func() {

	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should time out if the operator configuration resource has not been created", func() {
		startTime := time.Now()
		var elapsedTimeNanoseconds int64

		go func() {
			defer GinkgoRecover()
			Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(MatchError(
				"waiting for dash0-operator-configuration-auto-resource to become available has timed out " +
					"(no more retries left): dash0operatorconfigurations.operator.dash0.com " +
					"\"dash0-operator-configuration-auto-resource\" not found",
			))
			elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
		}()

		Eventually(func(g Gomega) {
			verifyTimeout(g, elapsedTimeNanoseconds)
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should time out if the operator configuration resource has been created but is not available", func() {
		startTime := time.Now()
		var elapsedTimeNanoseconds int64

		CreateOperatorConfigurationAutoResource(ctx, k8sClient)

		go func() {
			defer GinkgoRecover()
			Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(MatchError(
				"waiting for dash0-operator-configuration-auto-resource to become available has timed out " +
					"(no more retries left): the Dash0OperatorConfiguration resource " +
					"dash0-operator-configuration-auto-resource is not marked as available",
			))
			elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
		}()

		Eventually(func(g Gomega) {
			verifyTimeout(g, elapsedTimeNanoseconds)
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should terminate without error if the operator configuration resource has been created and is available", func() {
		operatorConfigurationResource := CreateOperatorConfigurationAutoResource(ctx, k8sClient)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())
		Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(Succeed())
	})
})

func verifyTimeout(g Gomega, elapsedTimeNanoseconds int64) {
	g.Expect(elapsedTimeNanoseconds).ToNot(BeZero())
	g.Expect(elapsedTimeNanoseconds).To(
		BeNumerically(
			">=",
			expectedPostInstallHandlerTimeoutNanosecondsMin,
		))
	g.Expect(elapsedTimeNanoseconds).To(
		BeNumerically(
			"<=",
			expectedPostInstallHandlerTimeoutNanosecondsMax,
		))
}
