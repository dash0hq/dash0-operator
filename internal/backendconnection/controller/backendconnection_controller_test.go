// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	backendconnectionv1alpha1 "github.com/dash0hq/dash0-operator/api/backendconnection/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	namespace = TestNamespaceName

	timeout         = 10 * time.Second
	pollingInterval = 50 * time.Millisecond

	extraBackendConnectionResourceNames = []types.NamespacedName{}
)

var _ = Describe("The BackendConnection Controller", Ordered, func() {

	ctx := context.Background()
	var createdObjects []client.Object

	var reconciler *BackendConnectionReconciler

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		reconciler = &BackendConnectionReconciler{
			Client:    k8sClient,
			ClientSet: clientset,
			Scheme:    k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, namespace)
	})

	Describe("when the Dash0 backend connection resource exists", func() {
		BeforeEach(func() {
			EnsureBackendConnectionResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			RemoveBackendConnectionResource(ctx, k8sClient)
			for _, name := range extraBackendConnectionResourceNames {
				RemoveBackendConnectionResourceByName(ctx, k8sClient, name, true)
			}
		})

		Describe("when reconciling", func() {
			It("should successfully run the first reconcile (no modifiable workloads exist)", func() {
				By("Trigger reconcile request")
				triggerReconcileRequest(ctx, reconciler, "")
				verifyBackendConnectionResourceIsAvailable(ctx)
			})

			It("should successfully run multiple reconciles (no modifiable workloads exist)", func() {
				triggerReconcileRequest(ctx, reconciler, "First reconcile request")

				firstAvailableStatusCondition := verifyBackendConnectionResourceIsAvailable(ctx)
				originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

				time.Sleep(50 * time.Millisecond)

				triggerReconcileRequest(ctx, reconciler, "Second reconcile request")

				// The LastTransitionTime should not change with subsequent reconciliations.
				secondAvailableCondition := verifyBackendConnectionResourceIsAvailable(ctx)
				Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
			})

			It("should mark only the most recent resource as available and the other ones as degraded when multiple "+
				"resources exist", func() {
				firstBackendConnectionResource := &backendconnectionv1alpha1.BackendConnection{}
				Expect(k8sClient.Get(ctx, BackendConnectionResourceQualifiedName, firstBackendConnectionResource)).To(Succeed())
				time.Sleep(10 * time.Millisecond)
				secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-backendconnection-test-resource-2"}
				extraBackendConnectionResourceNames = append(extraBackendConnectionResourceNames, secondName)
				CreateBackendConnectionResource(ctx, k8sClient, secondName)
				time.Sleep(10 * time.Millisecond)
				thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-backendconnection-test-resource-3"}
				extraBackendConnectionResourceNames = append(extraBackendConnectionResourceNames, thirdName)
				CreateBackendConnectionResource(ctx, k8sClient, thirdName)

				triggerReconcileRequestForName(ctx, reconciler, "", BackendConnectionResourceQualifiedName)
				triggerReconcileRequestForName(ctx, reconciler, "", secondName)
				triggerReconcileRequestForName(ctx, reconciler, "", thirdName)

				Eventually(func(g Gomega) {
					resource1Available := loadCondition(ctx, BackendConnectionResourceQualifiedName, util.ConditionTypeAvailable)
					resource1Degraded := loadCondition(ctx, BackendConnectionResourceQualifiedName, util.ConditionTypeDegraded)
					resource2Available := loadCondition(ctx, secondName, util.ConditionTypeAvailable)
					resource2Degraded := loadCondition(ctx, secondName, util.ConditionTypeDegraded)
					resource3Available := loadCondition(ctx, thirdName, util.ConditionTypeAvailable)
					resource3Degraded := loadCondition(ctx, thirdName, util.ConditionTypeDegraded)

					// The first two resource should have been marked as degraded.
					verifyCondition(
						g,
						resource1Available,
						metav1.ConditionFalse,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 backend connection resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(
						g,
						resource1Degraded,
						metav1.ConditionTrue,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 backend connection resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(g, resource2Available, metav1.ConditionFalse, "NewerResourceIsPresent",
						"There is a more recently created Dash0 backend connection resource in this namespace, please remove all "+
							"but one resource instance.")
					verifyCondition(g, resource2Degraded, metav1.ConditionTrue, "NewerResourceIsPresent",
						"There is a more recently created Dash0 backend connection resource in this namespace, please remove all "+
							"but one resource instance.")

					// The third (and most recent) resource should have been marked as available.
					verifyCondition(
						g,
						resource3Available,
						metav1.ConditionTrue,
						"ReconcileFinished",
						"A Dash0 backend connection has been created in this namespace.",
					)
					g.Expect(resource3Degraded).To(BeNil())

				}, timeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when creating OpenTelemetry collector resources", func() {
		})
	})
})

func triggerReconcileRequest(ctx context.Context, reconciler *BackendConnectionReconciler, stepMessage string) {
	triggerReconcileRequestForName(ctx, reconciler, stepMessage, BackendConnectionResourceQualifiedName)
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *BackendConnectionReconciler,
	stepMessage string,
	backendConnectionResourceName types.NamespacedName,
) {
	if stepMessage == "" {
		stepMessage = "Trigger reconcile request"
	}
	By(stepMessage)
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: backendConnectionResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyBackendConnectionResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		backendConnectionResource := LoadBackendConnectionResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(
			backendConnectionResource.Status.Conditions,
			string(util.ConditionTypeAvailable),
		)
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(backendConnectionResource.Status.Conditions, string(util.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func loadCondition(ctx context.Context, backendConnectionResourceName types.NamespacedName, conditionType util.ConditionType) *metav1.Condition {
	backendConnectionResource := LoadBackendConnectionResourceByNameOrFail(ctx, k8sClient, Default, backendConnectionResourceName)
	return meta.FindStatusCondition(backendConnectionResource.Status.Conditions, string(conditionType))
}

func verifyCondition(
	g Gomega,
	condition *metav1.Condition,
	expectedStatus metav1.ConditionStatus,
	expectedReason string,
	expectedMessage string,
) {
	g.Expect(condition).NotTo(BeNil())
	g.Expect(condition.Status).To(Equal(expectedStatus))
	g.Expect(condition.Reason).To(Equal(expectedReason))
	g.Expect(condition.Message).To(Equal(expectedMessage))
}
