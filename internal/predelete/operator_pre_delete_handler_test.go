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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/util"

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
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName1)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)
		Eventually(func(g Gomega) {
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)
		}, testTimeout, pollingInterval).Should(Succeed())

		EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, dash0MonitoringResourceName2)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)

		waitForDeletionRequest(ctx, dash0MonitoringResourceName2)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)
		Eventually(func(g Gomega) {
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName1)
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)
		}, testTimeout, pollingInterval).Should(Succeed())
		Eventually(handlerHasFinished, testTimeout).Should(BeClosed())
	})

	It("should disable automatic namespace monitoring and wait for auto-monitoring resources to be removed before "+
		"deleting the remaining resources", func() {
		enableAutoNamespaceMonitoring(ctx)
		markAsAutoMonitoringResource(ctx, dash0MonitoringResourceName1)

		manualResourceWasDeletedPrematurely := make(chan bool, 1)
		go func() {
			defer GinkgoRecover()
			simulateOperatorRemovingAutoMonitoringResourceAfterAutoMonitoringHasBeenDisabled(
				ctx,
				manualResourceWasDeletedPrematurely,
			)
		}()

		handlerHasFinished := runPreDeleteHandler()

		waitForDeletionRequest(ctx, dash0MonitoringResourceName2)
		triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName2)

		var deletedPrematurely bool
		Eventually(manualResourceWasDeletedPrematurely, testTimeout).Should(Receive(&deletedPrematurely))
		Expect(deletedPrematurely).To(BeFalse())
		Eventually(func(g Gomega) {
			operatorConfiguration := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
			g.Expect(operatorConfiguration.Spec.AutoMonitorNamespaces.IsEnabled()).To(BeFalse())

			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName1)
			VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, dash0MonitoringResourceName2)

			deployment1 := GetDeploymentEventually(ctx, k8sClient, g, deployment1.Namespace, deployment1.Name)
			VerifyUnmodifiedDeploymentEventually(g, deployment1)
			deployment2 := GetDeploymentEventually(ctx, k8sClient, g, deployment2.Namespace, deployment2.Name)
			VerifyUnmodifiedDeploymentEventually(g, deployment2)
		}, testTimeout, pollingInterval).Should(Succeed())
		Eventually(handlerHasFinished, testTimeout).Should(BeClosed())
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

func enableAutoNamespaceMonitoring(ctx context.Context) {
	operatorConfiguration := &dash0v1alpha1.Dash0OperatorConfiguration{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: OperatorConfigurationResourceName}, operatorConfiguration)).
		To(Succeed())
	operatorConfiguration.Spec.AutoMonitorNamespaces.Enabled = new(true)
	Expect(k8sClient.Update(ctx, operatorConfiguration)).To(Succeed())
}

func markAsAutoMonitoringResource(ctx context.Context, name types.NamespacedName) {
	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
	Expect(k8sClient.Get(ctx, name, monitoringResource)).To(Succeed())
	if monitoringResource.Labels == nil {
		monitoringResource.Labels = map[string]string{}
	}
	monitoringResource.Labels[util.AutoMonitoredNamespaceLabel] = util.TrueString
	Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
}

// simulateOperatorRemovingAutoMonitoringResourceAfterAutoMonitoringHasBeenDisabled mimics the auto-namespace
// monitoring controller and reports whether the manually created resource was already being deleted at that point.
func simulateOperatorRemovingAutoMonitoringResourceAfterAutoMonitoringHasBeenDisabled(
	ctx context.Context,
	manualResourceWasDeletedPrematurely chan<- bool,
) {
	Eventually(func(g Gomega) {
		operatorConfiguration := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
		g.Expect(operatorConfiguration.Spec.AutoMonitorNamespaces.IsEnabled()).To(BeFalse())
	}, testTimeout, pollingInterval).Should(Succeed())

	manualResource := &dash0v1beta1.Dash0Monitoring{}
	Expect(k8sClient.Get(ctx, dash0MonitoringResourceName2, manualResource)).To(Succeed())
	manualResourceWasDeletedPrematurely <- manualResource.DeletionTimestamp != nil

	autoMonitoringResource := &dash0v1beta1.Dash0Monitoring{}
	Expect(k8sClient.Get(ctx, dash0MonitoringResourceName1, autoMonitoringResource)).To(Succeed())
	Expect(k8sClient.Delete(ctx, autoMonitoringResource)).To(Succeed())
	triggerReconcileRequestForName(ctx, reconciler, dash0MonitoringResourceName1)
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
