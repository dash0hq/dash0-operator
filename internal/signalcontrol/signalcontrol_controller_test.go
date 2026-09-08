// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"
	"time"

	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/scresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const signalControlResourceNameTest = "dash0-signal-control-test"

var _ = Describe("The Signal Control controller", Ordered, func() {
	ctx := context.Background()
	scRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: signalControlResourceNameTest}}
	edgeProxyName := types.NamespacedName{Namespace: OperatorNamespace, Name: OTelCollectorNamePrefixTest + "-edge-proxy"}

	var reconciler *SignalControlReconciler

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		scResourceManager := scresources.NewSignalControlResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			OperatorNamespace,
			OTelCollectorNamePrefixTest,
			"edge-proxy-image:test",
			corev1.PullIfNotPresent,
			OperatorVersionTest,
			otelcolresources.DefaultOtlpGrpcHostPort,
			cluster.KubernetesVersionInfo{},
		)
		scManager := NewSignalControlManager(k8sClient, scResourceManager, nodeMetadataClient, util.ExtraConfigDefaults)
		oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			util.CollectorConfig{
				Images:                    TestImages,
				OperatorNamespace:         OperatorNamespace,
				OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
				TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
			},
		)
		collectorManager := collectors.NewCollectorManager(
			k8sClient,
			nodeMetadataClient,
			util.ExtraConfigDefaults,
			false,
			true,
			oTelColResourceManager,
		)
		reconciler = NewSignalControlReconciler(k8sClient, scManager, collectorManager)

		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, dash0v1alpha1.Dash0OperatorConfigurationSpec{
			Exports: []dash0common.Export{
				{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint:      EndpointDash0Test,
						ApiEndpoint:   ApiEndpointTest,
						Authorization: dash0common.Authorization{Token: &AuthorizationTokenTest},
					},
				},
			},
		})
		Expect(k8sClient.Create(ctx, &dash0v1alpha1.Dash0SignalControl{
			ObjectMeta: metav1.ObjectMeta{Name: signalControlResourceNameTest},
			Spec: dash0v1alpha1.Dash0SignalControlSpec{
				Enabled:   ptr.To(true),
				EdgeProxy: dash0v1alpha1.EdgeProxyConfig{Enabled: ptr.To(true)},
			},
		})).To(Succeed())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, &dash0v1alpha1.Dash0SignalControl{
			ObjectMeta: metav1.ObjectMeta{Name: signalControlResourceNameTest},
		})
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.DaemonSet{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	It("marks the resource available and deploys the Edge Proxy when Signal Control is enabled", func() {
		_, err := reconciler.Reconcile(ctx, scRequest)
		Expect(err).ToNot(HaveOccurred())

		signalControlResource := loadSignalControlResource(ctx)
		Expect(signalControlResource.IsAvailable()).To(BeTrue())
		Expect(signalControlResource.IsDegraded()).To(BeFalse())

		By("verifying the Edge Proxy deployment has been created")
		Expect(k8sClient.Get(ctx, edgeProxyName, &appsv1.Deployment{})).To(Succeed())
	})

	It("marks the resource degraded and does not deploy the Edge Proxy when no Dash0 export is configured", func() {
		By("replacing the operator configuration with one that has no Dash0 export")
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(false)},
			Exports:        []dash0common.Export{*HttpExportTest()},
		})

		// The reconcile returns an error to requeue until a Dash0 export is configured.
		_, err := reconciler.Reconcile(ctx, scRequest)
		Expect(err).To(HaveOccurred())

		signalControlResource := loadSignalControlResource(ctx)
		Expect(signalControlResource.IsDegraded()).To(BeTrue())
		degraded := meta.FindStatusCondition(
			signalControlResource.Status.Conditions,
			string(dash0common.ConditionTypeDegraded),
		)
		Expect(degraded).ToNot(BeNil())
		Expect(degraded.Reason).To(Equal(reasonSignalControlNoDash0Export))

		By("verifying no Edge Proxy deployment has been created")
		err = k8sClient.Get(ctx, edgeProxyName, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

})

func loadSignalControlResource(ctx context.Context) *dash0v1alpha1.Dash0SignalControl {
	signalControlResource := &dash0v1alpha1.Dash0SignalControl{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: signalControlResourceNameTest}, signalControlResource)).
		To(Succeed())
	return signalControlResource
}

var _ = Describe("Edge Proxy availability zone coverage", func() {
	ctx := context.Background()

	var warnings []string
	var recordingLogger logd.Logger

	BeforeEach(func() {
		warnings = nil
		recordingLogger = logd.NewLogger(funcr.New(func(_ string, args string) {
			warnings = append(warnings, args)
		}, funcr.Options{}))
	})

	It("warns with the Edge Proxy wording when there are more zones than replicas, and clears it when resolved", func() {
		for _, zone := range []string{"ep-zone-a", "ep-zone-b", "ep-zone-c"} {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "edge-proxy-zone-coverage-" + zone,
					Labels: map[string]string{corev1.LabelTopologyZone: zone},
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, node))).To(Succeed())
			})
		}

		currentTime := time.Unix(0, 0)
		manager := &SignalControlManager{}
		twoReplicas := util.ExtraConfig{EdgeProxyReplicas: 2}
		threeReplicas := util.ExtraConfig{EdgeProxyReplicas: 3}

		// A fresh reporter on every attempt makes each one a first check that lists the nodes, working around watch
		// cache lag.
		Eventually(func(g Gomega) {
			warnings = nil
			manager.zoneCoverageReporter = cluster.NewZoneCoverageReporter(
				nodeMetadataClient, cluster.WithClock(func() time.Time { return currentTime }))
			manager.warnAboutInsufficientZoneCoverage(ctx, twoReplicas, recordingLogger)
			g.Expect(warnings).To(HaveLen(1))
			g.Expect(warnings[0]).To(ContainSubstring("3 availability zones"))
			g.Expect(warnings[0]).To(ContainSubstring("Edge Proxy runs with 2 replicas"))
			g.Expect(warnings[0]).To(ContainSubstring("operator.signalControl.edgeProxy.replicas"))
		}).Should(Succeed())

		// A replica change bypasses the interval: now sufficient, it logs the resolution with the Edge Proxy wording.
		warnings = nil
		manager.warnAboutInsufficientZoneCoverage(ctx, threeReplicas, recordingLogger)
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0]).To(ContainSubstring("Edge Proxy now runs with 3 replicas"))
	})
})
