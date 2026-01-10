// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package predelete

import (
	"context"
	"fmt"
	"time"

	appv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/controller"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	namespace1      = "test-namespace-1"
	namespace2      = "test-namespace-2"
	testTimeout     = 10 * time.Second
	pollingInterval = 100 * time.Millisecond
)

var (
	dash0MonitoringResourceName1 = types.NamespacedName{
		Namespace: namespace1,
		Name:      MonitoringResourceName,
	}
	dash0MonitoringResourceName2 = types.NamespacedName{
		Namespace: namespace2,
		Name:      MonitoringResourceName,
	}
)

var _ = Describe("Uninstalling the Dash0 operator", Ordered, func() {

	ctx := context.Background()
	var (
		createdObjectsPreDeleteHandlerTest []client.Object
		deployment1                        *appv1.Deployment
		deployment2                        *appv1.Deployment
	)

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		createdObjectsPreDeleteHandlerTest, deployment1 = setupNamespaceWithDash0MonitoringResourceAndWorkload(
			ctx,
			k8sClient,
			dash0MonitoringResourceName1,
			createdObjectsPreDeleteHandlerTest,
		)
		createdObjectsPreDeleteHandlerTest, deployment2 = setupNamespaceWithDash0MonitoringResourceAndWorkload(
			ctx,
			k8sClient,
			dash0MonitoringResourceName2,
			createdObjectsPreDeleteHandlerTest,
		)
	})

	AfterEach(func() {
		createdObjectsPreDeleteHandlerTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsPreDeleteHandlerTest)
		DeleteMonitoringResourceByName(ctx, k8sClient, dash0MonitoringResourceName1, false)
		DeleteMonitoringResourceByName(ctx, k8sClient, dash0MonitoringResourceName2, false)
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should time out if the deletion of all Dash0 monitoring resources does not happen in a timely manner", func() {
		startTime := time.Now()
		var elapsedTimeNanoseconds int64

		go func() {
			defer GinkgoRecover()
			Expect(preDeleteHandler.DeleteAllMonitoringResources()).To(Succeed())
			elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
		}()

		// Deliberately not triggering a reconcile loop -> the finalizer action of the Dash0 monitoring resources will
		// not trigger, and the Dash0 monitoring resources won't be deleted. Ultimately, the timeout will kick in.

		Eventually(func(g Gomega) {
			g.Expect(elapsedTimeNanoseconds).ToNot(BeZero())
			g.Expect(elapsedTimeNanoseconds).To(BeNumerically("~", preDeleteHandlerTimeoutForTests, time.Second))
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should delete all Dash0 monitoring resources and uninstrument workloads", func() {
		go func() {
			defer GinkgoRecover()
			Expect(preDeleteHandler.DeleteAllMonitoringResources()).To(Succeed())
		}()

		// Triggering reconcile requests for both Dash0 monitoring resources to run cleanup actions and remove the
		// finalizer, so that the resources actually get deleted.
		go func() {
			defer GinkgoRecover()
			time.Sleep(500 * time.Millisecond)
			triggerReconcileRequestForName(
				ctx,
				reconciler,
				dash0MonitoringResourceName1,
			)
			triggerReconcileRequestForName(
				ctx,
				reconciler,
				dash0MonitoringResourceName2,
			)
		}()

		Eventually(func(g Gomega) {
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName1)
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)

			VerifySuccessfulUninstrumentationEventEventually(ctx, clientset, g, deployment1.Namespace, deployment1.Name, "controller")
			deployment1 := GetDeploymentEventually(ctx, k8sClient, g, deployment1.Namespace, deployment1.Name)
			VerifyUnmodifiedDeploymentEventually(g, deployment1)
			VerifyWebhookIgnoreOnceLabelIsPresentEventually(g, &deployment1.ObjectMeta)

			VerifySuccessfulUninstrumentationEventEventually(ctx, clientset, g, deployment2.Namespace, deployment2.Name, "controller")
			deployment2 := GetDeploymentEventually(ctx, k8sClient, g, deployment2.Namespace, deployment2.Name)
			VerifyUnmodifiedDeploymentEventually(g, deployment2)
			VerifyWebhookIgnoreOnceLabelIsPresentEventually(g, &deployment2.ObjectMeta)
		}, testTimeout, pollingInterval).Should(Succeed())
	})
})

func setupNamespaceWithDash0MonitoringResourceAndWorkload(
	ctx context.Context,
	k8sClient client.Client,
	dash0MonitoringResourceName types.NamespacedName,
	createdObjects []client.Object,
) ([]client.Object, *appv1.Deployment) {
	EnsureNamespaceExists(ctx, k8sClient, dash0MonitoringResourceName.Namespace)
	EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, dash0MonitoringResourceName)
	deploymentName := UniqueName(DeploymentNamePrefix)
	deployment := CreateInstrumentedDeployment(ctx, k8sClient, dash0MonitoringResourceName.Namespace, deploymentName)
	// make sure the monitoring resource has the finalizer
	triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName)
	return append(createdObjects, deployment), deployment
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *controller.MonitoringReconciler,
	dash0MonitoringResourceName types.NamespacedName,
) {
	By(fmt.Sprintf("Trigger reconcile request for %s/%s", dash0MonitoringResourceName.Namespace, dash0MonitoringResourceName.Name))
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: dash0MonitoringResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}
