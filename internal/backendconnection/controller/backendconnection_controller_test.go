// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	backendconnectionv1alpha1 "github.com/dash0hq/dash0-operator/api/backendconnection/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	namespace             = TestNamespaceName
	expectedConfigMapName = "dash0-opentelemetry-collector-daemonset"

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
		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client: k8sClient,
		}
		reconciler = &BackendConnectionReconciler{
			Client:                 k8sClient,
			ClientSet:              clientset,
			Scheme:                 k8sClient.Scheme(),
			OTelColResourceManager: oTelColResourceManager,
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
			RemoveBackendConnectionResourceByName(ctx, k8sClient, BackendConnectionResourceQualifiedName, false)
			for _, name := range extraBackendConnectionResourceNames {
				RemoveBackendConnectionResourceByName(ctx, k8sClient, name, true)
			}
			err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(namespace))
			Expect(err).ToNot(HaveOccurred())
		})

		Describe("when reconciling", func() {
			It("should successfully run the first reconcile", func() {
				triggerReconcileRequest(ctx, reconciler)
				verifyBackendConnectionResourceIsAvailable(ctx)
			})

			It("should successfully run multiple reconciles", func() {
				triggerReconcileRequest(ctx, reconciler)

				firstAvailableStatusCondition := verifyBackendConnectionResourceIsAvailable(ctx)
				originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

				time.Sleep(50 * time.Millisecond)

				triggerReconcileRequest(ctx, reconciler)

				// The LastTransitionTime should not change with subsequent reconciliations.
				secondAvailableCondition := verifyBackendConnectionResourceIsAvailable(ctx)
				Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
			})

			It("should mark only the most recent resource as available and the other ones as degraded when multiple "+
				"resources exist", func() {
				firstBackendConnectionResource := &backendconnectionv1alpha1.BackendConnection{}
				Expect(k8sClient.Get(ctx, BackendConnectionResourceQualifiedName, firstBackendConnectionResource)).To(Succeed())
				time.Sleep(10 * time.Millisecond)
				secondName := types.NamespacedName{Namespace: namespace, Name: "dash0-backendconnection-test-resource-2"}
				extraBackendConnectionResourceNames = append(extraBackendConnectionResourceNames, secondName)
				CreateBackendConnectionResource(ctx, k8sClient, secondName)
				time.Sleep(10 * time.Millisecond)
				thirdName := types.NamespacedName{Namespace: namespace, Name: "dash0-backendconnection-test-resource-3"}
				extraBackendConnectionResourceNames = append(extraBackendConnectionResourceNames, thirdName)
				CreateBackendConnectionResource(ctx, k8sClient, thirdName)

				triggerReconcileRequestForName(ctx, reconciler, BackendConnectionResourceQualifiedName)
				triggerReconcileRequestForName(ctx, reconciler, secondName)
				triggerReconcileRequestForName(ctx, reconciler, thirdName)

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
						"Resources for the Dash0 backend connection have been created in this namespace.",
					)
					g.Expect(resource3Degraded).To(BeNil())

				}, timeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when creating OpenTelemetry collector resources", func() {
			It("should create a config map", func() {
				triggerReconcileRequest(ctx, reconciler)
				verifyConfigMap(ctx)
				verifyResourcesHaveBeenCreated(ctx)
			})
		})

		Describe("when updating OpenTelemetry collector resources", func() {
			It("should update the config map", func() {
				err := k8sClient.Create(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      expectedConfigMapName,
						Namespace: namespace,
						Labels: map[string]string{
							"wrong-key": "value",
						},
						Annotations: map[string]string{
							"wrong-key": "value",
						},
					},
					Data: map[string]string{
						"wrong-key": "{}",
					},
				})
				Expect(err).ToNot(HaveOccurred())
				triggerReconcileRequest(ctx, reconciler)
				verifyConfigMap(ctx)
				verifyResourcesHaveBeenUpdated(ctx)
			})
		})

		Describe("when all OpenTelemetry collector resources are up to date", func() {
			It("should report that nothing has changed", func() {
				// create all resources (so we are sure that everything is in the desired state)
				triggerReconcileRequest(ctx, reconciler)

				// trigger another reconcile request, which should be a no-op
				triggerReconcileRequest(ctx, reconciler)
				verifyConfigMap(ctx)
				verifyResourcesHaveNotChanged(ctx)
			})
		})

		Describe("when cleaning up OpenTelemetry collector resources when the resource is deleted", func() {
			It("should delete the config map", func() {
				// We trigger one reconcile request before creating any workload and before deleting the backend connection
				// resource, just to create the OTel collector resources and to add the finalizer to the backend connection
				// resource.
				triggerReconcileRequest(ctx, reconciler)
				backendConnectionResource := LoadBackendConnectionResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, backendConnectionResource)).To(Succeed())

				// This reconcile request should clean up and delete all OTel collector resources.
				triggerReconcileRequest(ctx, reconciler)

				verifyNoConfigMapExists(ctx)
			})
		})
	})
})

func triggerReconcileRequest(
	ctx context.Context,
	reconciler *BackendConnectionReconciler,
) {
	triggerReconcileRequestForName(
		ctx,
		reconciler,
		BackendConnectionResourceQualifiedName,
	)
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *BackendConnectionReconciler,
	backendConnectionResourceName types.NamespacedName,
) {
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: backendConnectionResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyBackendConnectionResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
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

func verifyResourcesHaveBeenCreated(ctx context.Context) AsyncAssertion {
	return verifyResourceAndCondition(ctx, "Resources for the Dash0 backend connection have been created in this namespace.")
}

func verifyResourcesHaveBeenUpdated(ctx context.Context) AsyncAssertion {
	return verifyResourceAndCondition(ctx, "Resources for the Dash0 backend connection have been updated in this namespace.")
}

func verifyResourcesHaveNotChanged(ctx context.Context) AsyncAssertion {
	return verifyResourceAndCondition(ctx, "The resources for the Dash0 backend connection in this namespace are up to date.")
}

func verifyResourceAndCondition(ctx context.Context, expectedMessage string) AsyncAssertion {
	return Eventually(func(g Gomega) {
		availableCondition := verifyBackendConnectionResourceIsAvailable(ctx)
		verifyCondition(
			g,
			availableCondition,
			metav1.ConditionTrue,
			"ReconcileFinished",
			expectedMessage,
		)
	})
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

func verifyConfigMap(ctx context.Context) {
	key := client.ObjectKey{Name: expectedConfigMapName, Namespace: namespace}
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, key, cm)
	Expect(err).ToNot(HaveOccurred())
	Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
	Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
	Expect(cm.Data).To(HaveKey("collector.yaml"))
	Expect(cm.Data).ToNot(HaveKey("wrong-key"))
}

func verifyNoConfigMapExists(ctx context.Context) {
	key := client.ObjectKey{Name: expectedConfigMapName, Namespace: namespace}
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, key, cm)
	Expect(err).To(HaveOccurred(), "the config map still exists although it should have been deleted")
	Expect(apierrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("loading the config map failed with an unexpected error: %v", err))
}
