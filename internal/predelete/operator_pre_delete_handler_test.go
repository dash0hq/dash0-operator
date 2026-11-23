// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package predelete

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	appv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
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

var _ = Describe("Uninstalling the Dash0 operator (pre-delete hook)", Ordered, func() {

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
		var elapsedTimeNanoseconds atomic.Int64

		go func() {
			defer GinkgoRecover()
			Expect(preDeleteHandler.DeleteAllMonitoringResources()).To(Succeed())
			elapsedTimeNanoseconds.Store(time.Since(startTime).Nanoseconds())
		}()

		// Deliberately not triggering a reconcile loop -> the finalizer action of the Dash0 monitoring resources will
		// not trigger, and the Dash0 monitoring resources won't be deleted. Ultimately, the timeout will kick in.

		Eventually(func(g Gomega) {
			g.Expect(elapsedTimeNanoseconds.Load()).ToNot(BeZero())
			g.Expect(elapsedTimeNanoseconds.Load()).To(BeNumerically("~", preDeleteHandlerTimeoutForTests, time.Second))
		}, testTimeout, pollingInterval).Should(Succeed())
	})

	It("should delete all Dash0 monitoring resources and uninstrument workloads", func() {
		handlerHasFinished := runPreDeleteHandler()

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
		Eventually(handlerHasFinished, testTimeout).Should(BeClosed())
	})

	It("should delete monitoring resources again that have been recreated while waiting", func() {
		handlerHasFinished := runPreDeleteHandler()

		waitForDeletionRequest(ctx, dash0MonitoringResourceName1)
		waitForDeletionRequest(ctx, dash0MonitoringResourceName2)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)
		Eventually(func(g Gomega) {
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)
		}, testTimeout, pollingInterval).Should(Succeed())

		// The resource in namespace 1 is still pending deletion, which keeps the handler waiting. The recreated
		// resource carries the finalizer from the start, so the handler's deletion request cannot remove it right away.
		recreatedMonitoringResource := DefaultMonitoringResourceWithName(dash0MonitoringResourceName2)
		controllerutil.AddFinalizer(recreatedMonitoringResource, dash0common.MonitoringFinalizerId)
		CreateMonitoringResource(ctx, k8sClient, recreatedMonitoringResource)

		waitForDeletionRequest(ctx, dash0MonitoringResourceName2)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName1)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)
		Eventually(func(g Gomega) {
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName1)
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)
		}, testTimeout, pollingInterval).Should(Succeed())
		Eventually(handlerHasFinished, testTimeout).Should(BeClosed())
	})
})

var _ = Describe("Uninstalling the Dash0 operator (dataset finalizers)", func() {
	ctx := context.Background()
	datasetName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dataset"}

	It("should remove the finalizer from every Dash0 dataset resource", func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		dataset := &dash0dashv1alpha1.Dash0Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:       datasetName.Name,
				Namespace:  datasetName.Namespace,
				Finalizers: []string{dash0dashv1alpha1.DatasetFinalizerId},
			},
			Spec: dash0dashv1alpha1.Dash0DatasetSpec{
				Slug:    "staging",
				Display: dash0dashv1alpha1.Dash0DatasetDisplay{Name: "Staging"},
			},
		}
		Expect(k8sClient.Create(ctx, dataset)).To(Succeed())
		Expect(k8sClient.Delete(ctx, dataset)).To(Succeed())

		Expect(preDeleteHandler.RemoveAllDatasetFinalizers()).To(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, datasetName, &dash0dashv1alpha1.Dash0Dataset{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected the dataset resource to be gone, got %v", err)
		}, testTimeout, pollingInterval).Should(Succeed())
	})
})

func runPreDeleteHandler() <-chan struct{} {
	handlerHasFinished := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(handlerHasFinished)
		Expect(preDeleteHandler.DeleteAllMonitoringResources()).To(Succeed())
	}()
	return handlerHasFinished
}

func waitForDeletionRequest(ctx context.Context, name types.NamespacedName) {
	Eventually(func(g Gomega) {
		resource := LoadMonitoringResourceByNameIfItExists(ctx, k8sClient, g, name)
		g.Expect(resource).ToNot(BeNil())
		g.Expect(resource.DeletionTimestamp).ToNot(BeNil())
	}, testTimeout, pollingInterval).Should(Succeed())
}

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
