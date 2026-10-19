// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The dry-run requests of the webhook reachability check", Ordered, func() {

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		err := k8sClient.DeleteAllOf(ctx, &dash0v1beta1.Dash0Monitoring{}, client.InNamespace(OperatorNamespace))
		if !apierrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	operatorConfigurationResource := func(name string) *dash0v1alpha1.Dash0OperatorConfiguration {
		return &dash0v1alpha1.Dash0OperatorConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}

	monitoringResource := func(name string) *dash0v1beta1.Dash0Monitoring {
		return &dash0v1beta1.Dash0Monitoring{ObjectMeta: metav1.ObjectMeta{Namespace: OperatorNamespace, Name: name}}
	}

	It("should allow a dry-run creation of resources with the marker name, without validating them", func() {
		Expect(k8sClient.Create(
			ctx, operatorConfigurationResource(util.WebhookReachabilityCheckResourceName), client.DryRunAll,
		)).To(Succeed())
		Expect(k8sClient.Create(
			ctx, monitoringResource(util.WebhookReachabilityCheckResourceName), client.DryRunAll,
		)).To(Succeed())
	})

	It("should validate a dry-run creation of resources with another name", func() {
		Expect(k8sClient.Create(
			ctx, operatorConfigurationResource("dash0-operator-configuration-dry-run"), client.DryRunAll,
		)).To(MatchError(ContainSubstring("admission webhook \"validate-operator-configuration.dash0.com\" denied")))
		Expect(k8sClient.Create(
			ctx, monitoringResource("dash0-monitoring-dry-run"), client.DryRunAll,
		)).To(MatchError(ContainSubstring("admission webhook \"validate-monitoring.dash0.com\" denied")))
	})

	It("should validate a regular creation of resources with the marker name", func() {
		Expect(k8sClient.Create(
			ctx, operatorConfigurationResource(util.WebhookReachabilityCheckResourceName),
		)).To(MatchError(ContainSubstring("admission webhook \"validate-operator-configuration.dash0.com\" denied")))
		Expect(k8sClient.Create(
			ctx, monitoringResource(util.WebhookReachabilityCheckResourceName),
		)).To(MatchError(ContainSubstring("admission webhook \"validate-monitoring.dash0.com\" denied")))
	})
})
