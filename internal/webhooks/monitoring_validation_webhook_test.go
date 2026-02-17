// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// This file contains additional tests for the export/exports mutual exclusivity
// validation in the monitoring validation webhook. It should be added alongside the
// existing monitoring_validation_webhook_test.go file.

package webhooks

import (
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The validation webhook for the monitoring resource - export and exports mutual exclusivity", func() {

	AfterEach(func() {
		Expect(
			k8sClient.DeleteAllOf(ctx, &dash0v1beta1.Dash0Monitoring{}, client.InNamespace(TestNamespaceName)),
		).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
	})

	It("should reject monitoring resources with both export and exports set", func() {
		_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
			ObjectMeta: MonitoringResourceDefaultObjectMeta,
			Spec: dash0v1beta1.Dash0MonitoringSpec{
				Export:  Dash0ExportWithEndpointAndToken(),
				Exports: []dash0common.Export{*GrpcExportTest()},
			},
		})

		Expect(err).To(MatchError(ContainSubstring(ErrorMessageMonitoringExportAndExportsAreMutuallyExclusive)))
	})

	It("should allow monitoring resource creation with only exports set", func() {
		operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
			ctx,
			k8sClient,
			dash0v1alpha1.Dash0OperatorConfigurationSpec{
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
			},
		)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

		_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
			ObjectMeta: MonitoringResourceDefaultObjectMeta,
			Spec: dash0v1beta1.Dash0MonitoringSpec{
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
			},
		})

		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow monitoring resource creation with only export set (mutating webhook migrates it)", func() {
		_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
			ObjectMeta: MonitoringResourceDefaultObjectMeta,
			Spec: dash0v1beta1.Dash0MonitoringSpec{
				Export: Dash0ExportWithEndpointAndToken(),
			},
		})

		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow monitoring resource creation without export if operator configuration has exports", func() {
		operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
			ctx,
			k8sClient,
			dash0v1alpha1.Dash0OperatorConfigurationSpec{
				SelfMonitoring: dash0v1alpha1.SelfMonitoring{
					Enabled: ptr.To(false),
				},
				Exports: []dash0common.Export{*Dash0ExportWithEndpointAndToken()},
			},
		)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

		_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1beta1.Dash0Monitoring{
			ObjectMeta: MonitoringResourceDefaultObjectMeta,
			Spec: dash0v1beta1.Dash0MonitoringSpec{
				InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
					Mode: dash0common.InstrumentWorkloadsModeAll,
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
	})
})
