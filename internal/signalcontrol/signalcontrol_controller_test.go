// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"
	"net/http"

	"github.com/h2non/gock"
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
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/enablement"
	"github.com/dash0hq/dash0-operator/internal/signalcontrol/scresources"
	"github.com/dash0hq/dash0-operator/internal/util"

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
		// A client with a nil transport uses http.DefaultTransport, which gock replaces when a mock is registered.
		checker := enablement.NewEnablementChecker(&http.Client{}, k8sClient, OperatorNamespace)
		scResourceManager := scresources.NewSignalControlResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			OperatorNamespace,
			OTelCollectorNamePrefixTest,
			"edge-proxy-image:test",
			corev1.PullIfNotPresent,
			OperatorVersionTest,
		)
		scManager := NewSignalControlManager(k8sClient, scResourceManager, checker, util.ExtraConfigDefaults)
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
			clientset,
			util.ExtraConfigDefaults,
			false,
			true,
			checker,
			oTelColResourceManager,
		)
		reconciler = NewSignalControlReconciler(k8sClient, scManager, collectorManager, checker)

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
		gock.Off()
		_ = k8sClient.Delete(ctx, &dash0v1alpha1.Dash0SignalControl{
			ObjectMeta: metav1.ObjectMeta{Name: signalControlResourceNameTest},
		})
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.DaemonSet{}, client.InNamespace(OperatorNamespace))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	It("marks the resource available and deploys the Edge Proxy when the organization is entitled", func() {
		gock.New(ApiEndpointTest).
			Get("/api/signal-control/edge/settings").
			MatchHeader("Authorization", AuthorizationHeaderTest).
			Reply(http.StatusOK).
			JSON(map[string]bool{"enabled": true})

		_, err := reconciler.Reconcile(ctx, scRequest)
		Expect(err).ToNot(HaveOccurred())
		Expect(gock.IsDone()).To(BeTrue())

		signalControlResource := loadSignalControlResource(ctx)
		Expect(signalControlResource.IsAvailable()).To(BeTrue())
		Expect(signalControlResource.IsDegraded()).To(BeFalse())

		By("verifying the Edge Proxy deployment has been created")
		Expect(k8sClient.Get(ctx, edgeProxyName, &appsv1.Deployment{})).To(Succeed())
	})

	It("marks the resource degraded and does not deploy the Edge Proxy when the organization is not entitled", func() {
		gock.New(ApiEndpointTest).
			Get("/api/signal-control/edge/settings").
			Reply(http.StatusOK).
			JSON(map[string]bool{"enabled": false})

		_, err := reconciler.Reconcile(ctx, scRequest)
		Expect(err).ToNot(HaveOccurred())
		Expect(gock.IsDone()).To(BeTrue())

		signalControlResource := loadSignalControlResource(ctx)
		Expect(signalControlResource.IsDegraded()).To(BeTrue())
		degraded := meta.FindStatusCondition(
			signalControlResource.Status.Conditions,
			string(dash0common.ConditionTypeDegraded),
		)
		Expect(degraded).ToNot(BeNil())
		Expect(degraded.Reason).To(Equal(reasonSignalControlNotEnabledForOrganization))

		By("verifying no Edge Proxy deployment has been created")
		err = k8sClient.Get(ctx, edgeProxyName, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("marks the resource degraded and does not deploy the Edge Proxy when no Dash0 export is configured", func() {
		By("replacing the operator configuration with one that has no Dash0 export")
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: ptr.To(false)},
			Exports:        []dash0common.Export{*HttpExportTest()},
		})

		// No entitlement mock is registered: the Dash0-export precheck happens before the entitlement HTTP check,
		// so no request is made. The reconcile returns an error to requeue until a Dash0 export is configured.
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

	It("marks the resource degraded and requeues when the entitlement check cannot be completed", func() {
		gock.New(ApiEndpointTest).
			Get("/api/signal-control/edge/settings").
			Persist().
			Reply(http.StatusServiceUnavailable)

		_, err := reconciler.Reconcile(ctx, scRequest)
		Expect(err).To(HaveOccurred())

		signalControlResource := loadSignalControlResource(ctx)
		Expect(signalControlResource.IsDegraded()).To(BeTrue())
		degraded := meta.FindStatusCondition(
			signalControlResource.Status.Conditions,
			string(dash0common.ConditionTypeDegraded),
		)
		Expect(degraded).ToNot(BeNil())
		Expect(degraded.Reason).To(Equal(reasonSignalControlEnablementCheckFailed))

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
