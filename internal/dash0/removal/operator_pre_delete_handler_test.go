// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package removal

import (
	"context"
	"fmt"
	"time"

	appv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/dash0/controller"

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
	dash0CustomResourceName1 = types.NamespacedName{
		Namespace: namespace1,
		Name:      Dash0CustomResourceName,
	}
	dash0CustomResourceName2 = types.NamespacedName{
		Namespace: namespace2,
		Name:      Dash0CustomResourceName,
	}
)

var _ = Describe("Uninstalling the Dash0 Kubernetes operator", Ordered, func() {

	ctx := context.Background()
	var (
		createdObjects []client.Object
		deployment1    *appv1.Deployment
		deployment2    *appv1.Deployment
	)

	BeforeAll(func() {
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects, deployment1 = setupNamespaceWithDash0CustomResourceAndWorkload(
			ctx,
			k8sClient,
			dash0CustomResourceName1,
			createdObjects,
		)
		createdObjects, deployment2 = setupNamespaceWithDash0CustomResourceAndWorkload(
			ctx,
			k8sClient,
			dash0CustomResourceName2,
			createdObjects,
		)
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		RemoveDash0CustomResourceByName(ctx, k8sClient, dash0CustomResourceName1, false)
		RemoveDash0CustomResourceByName(ctx, k8sClient, dash0CustomResourceName2, false)
	})

	It("should time out if the deletion of all Dash0 custom resources does not happen in a timely manner", func() {
		startTime := time.Now()
		preDeleteHandlerTerminatedAt := time.Time{}

		go func() {
			defer GinkgoRecover()
			Expect(preDeleteHandler.DeleteAllDash0CustomResources()).To(Succeed())
			preDeleteHandlerTerminatedAt = time.Now()
		}()

		// Deliberately not triggering a reconcile loop -> the finalizer action of the Dash0 custom resources will
		// not trigger, and the Dash0 custom resources won't be deleted. Ultimately, the timeout will kick in.

		Eventually(func(g Gomega) {
			g.Expect(preDeleteHandlerTerminatedAt).ToNot(BeZero())
			elapsedTime := preDeleteHandlerTerminatedAt.Sub(startTime).Nanoseconds()
			g.Expect(elapsedTime).To(BeNumerically("~", preDeleteHandlerTimeoutForTests, time.Second))
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should delete all Dash0 custom resources and uninstrument workloads", func() {
		go func() {
			defer GinkgoRecover()
			Expect(preDeleteHandler.DeleteAllDash0CustomResources()).To(Succeed())
		}()

		// Triggering reconcile requests for both Dash0 custom resources to run cleanup actions and remove the
		// finalizer, so that the resources actually get deleted.
		go func() {
			defer GinkgoRecover()
			time.Sleep(500 * time.Millisecond)
			triggerReconcileRequestForName(
				ctx,
				reconciler,
				dash0CustomResourceName1,
			)
			triggerReconcileRequestForName(
				ctx,
				reconciler,
				dash0CustomResourceName2,
			)
		}()

		Eventually(func(g Gomega) {
			VerifyDash0CustomResourceByNameDoesNotExist(ctx, k8sClient, g, dash0CustomResourceName1)
			VerifyDash0CustomResourceByNameDoesNotExist(ctx, k8sClient, g, dash0CustomResourceName2)

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

func setupNamespaceWithDash0CustomResourceAndWorkload(
	ctx context.Context,
	k8sClient client.Client,
	dash0CustomResourceName types.NamespacedName,
	createdObjects []client.Object,
) ([]client.Object, *appv1.Deployment) {
	EnsureNamespaceExists(ctx, k8sClient, dash0CustomResourceName.Namespace)
	EnsureDash0CustomResourceExistsAndIsAvailableInNamespace(ctx, k8sClient, dash0CustomResourceName)
	deploymentName := UniqueName(DeploymentNamePrefix)
	deployment := CreateInstrumentedDeployment(ctx, k8sClient, dash0CustomResourceName.Namespace, deploymentName)
	// make sure the custom resource has the finalizer
	triggerReconcileRequestForName(ctx, reconciler, dash0CustomResourceName)
	return append(createdObjects, deployment), deployment
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *controller.Dash0Reconciler,
	dash0CustomResourceName types.NamespacedName,
) {
	By(fmt.Sprintf("Trigger reconcile request for %s/%s", dash0CustomResourceName.Namespace, dash0CustomResourceName.Name))
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: dash0CustomResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}
