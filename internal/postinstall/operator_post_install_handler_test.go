// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postinstall

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

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
		var elapsedTimeNanoseconds atomic.Int64

		go func() {
			defer GinkgoRecover()
			Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(MatchError(
				"waiting for dash0-operator-configuration-auto-resource to become available has timed out " +
					"(no more retries left): dash0operatorconfigurations.operator.dash0.com " +
					"\"dash0-operator-configuration-auto-resource\" not found",
			))
			elapsedTimeNanoseconds.Store(time.Since(startTime).Nanoseconds())
		}()

		Eventually(func(g Gomega) {
			verifyTimeout(g, elapsedTimeNanoseconds.Load())
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should time out if the operator configuration resource has been created but is not available", func() {
		startTime := time.Now()
		var elapsedTimeNanoseconds atomic.Int64

		CreateOperatorConfigurationAutoResource(ctx, k8sClient)

		go func() {
			defer GinkgoRecover()
			Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(MatchError(
				"waiting for dash0-operator-configuration-auto-resource to become available has timed out " +
					"(no more retries left): the Dash0OperatorConfiguration resource " +
					"dash0-operator-configuration-auto-resource is not marked as available",
			))
			elapsedTimeNanoseconds.Store(time.Since(startTime).Nanoseconds())
		}()

		Eventually(func(g Gomega) {
			verifyTimeout(g, elapsedTimeNanoseconds.Load())
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should terminate without error if the operator configuration resource has been created and is available", func() {
		operatorConfigurationResource := CreateOperatorConfigurationAutoResource(ctx, k8sClient)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())
		Expect(postInstallHandler.WaitForOperatorConfigurationResourceToBecomeAvailable()).To(Succeed())
	})
})

var _ = Describe("Waiting for the operator webhooks to become reachable", Ordered, func() {

	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(
			k8sClient.Delete(ctx, BasicDeployment(OperatorNamespace, OperatorManagerDeploymentName)),
		)).To(Succeed())
	})

	updateRolloutStatus := func(deployment *appsv1.Deployment, status appsv1.DeploymentStatus) {
		status.ObservedGeneration = deployment.Generation
		deployment.Status = status
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
	}

	It("should time out if the rollout of the operator manager deployment is not complete", func() {
		CreateBasicDeployment(ctx, k8sClient, OperatorNamespace, OperatorManagerDeploymentName)

		Expect(postInstallHandler.WaitForWebhooksToBecomeReachable(
			ctx, OperatorNamespace, OperatorManagerDeploymentName,
		)).To(MatchError(ContainSubstring("the operator manager deployment rollout is not complete")))
	})

	It("should fail without further retries if the rollout of the operator manager deployment has failed", func() {
		deployment := CreateBasicDeployment(ctx, k8sClient, OperatorNamespace, OperatorManagerDeploymentName)
		updateRolloutStatus(deployment, appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionFalse,
				Reason: "ProgressDeadlineExceeded",
			}},
		})
		handler, err := NewOperatorPostInstallHandlerFromConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		handler.reachabilityRetryBackoff = wait.Backoff{Duration: 10 * time.Second, Factor: 1, Steps: 10}

		startTime := time.Now()
		Expect(handler.WaitForWebhooksToBecomeReachable(
			ctx, OperatorNamespace, OperatorManagerDeploymentName,
		)).To(MatchError(ContainSubstring("the operator manager deployment rollout has failed")))
		Expect(time.Since(startTime)).To(BeNumerically("<", 5*time.Second))
	})

	It("should terminate without error once the rollout is complete and the dry-run requests succeed", func() {
		deployment := CreateBasicDeployment(ctx, k8sClient, OperatorNamespace, OperatorManagerDeploymentName)
		updateRolloutStatus(deployment, appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			ReadyReplicas:     1,
			AvailableReplicas: 1,
		})

		Expect(postInstallHandler.WaitForWebhooksToBecomeReachable(
			ctx, OperatorNamespace, OperatorManagerDeploymentName,
		)).To(Succeed())

		Expect(apierrors.IsNotFound(k8sClient.Get(
			ctx,
			client.ObjectKey{Name: util.WebhookReachabilityCheckResourceName},
			&dash0v1alpha1.Dash0OperatorConfiguration{},
		))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(
			ctx,
			client.ObjectKey{Namespace: OperatorNamespace, Name: util.WebhookReachabilityCheckResourceName},
			&dash0v1beta1.Dash0Monitoring{},
		))).To(BeTrue())
	})
})

var _ = DescribeTable("Classifying the result of a dry-run request", func(err error, expectReachable bool) {
	if expectReachable {
		Expect(classifyDryRunError(err)).To(Succeed())
	} else {
		Expect(classifyDryRunError(err)).ToNot(Succeed())
	}
},
	Entry("success", nil, true),
	Entry(
		"a Dash0 admission webhook cannot be called",
		apierrors.NewInternalError(fmt.Errorf(
			`failed calling webhook "validate-operator-configuration.dash0.com": failed to call webhook: Post `+
				`"https://dash0-operator-webhook-service.dash0-system.svc:443/operator-configuration/validate?timeout=5s": `+
				`tls: failed to verify certificate: x509: certificate signed by unknown authority`,
		)),
		false,
	),
	Entry(
		"the Dash0 conversion webhook cannot be called",
		apierrors.NewInternalError(fmt.Errorf(
			`conversion webhook for operator.dash0.com/v1alpha1, Kind=Dash0Monitoring failed: Post `+
				`"https://dash0-operator-webhook-service.dash0-system.svc:443/v1alpha1/convert": no endpoints available`,
		)),
		false,
	),
	Entry("a connection error", fmt.Errorf("dial tcp 10.0.0.1:443: connect: connection refused"), false),
	Entry("a timeout", context.DeadlineExceeded, false),
	Entry(
		"a third-party webhook cannot be called",
		apierrors.NewInternalError(fmt.Errorf(
			`failed calling webhook "validate.kyverno.svc-fail": failed to call webhook: context deadline exceeded`,
		)),
		true,
	),
	Entry(
		"a third-party webhook denies the request",
		apierrors.NewForbidden(
			schema.GroupResource{Group: "operator.dash0.com", Resource: "dash0monitorings"},
			util.WebhookReachabilityCheckResourceName,
			fmt.Errorf(`admission webhook "policy.example.com" denied the request`),
		),
		true,
	),
	Entry(
		"a validation error",
		apierrors.NewInvalid(
			schema.GroupKind{Group: "operator.dash0.com", Kind: "Dash0Monitoring"},
			util.WebhookReachabilityCheckResourceName,
			nil,
		),
		true,
	),
)

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
