// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
)

var (
	timeout         = 15 * time.Second
	pollingInterval = 50 * time.Millisecond
)

var _ = Describe("Dash0 Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		dash0 := &operatorv1alpha1.Dash0{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Dash0")
			err := k8sClient.Get(ctx, typeNamespacedName, dash0)
			if err != nil && errors.IsNotFound(err) {
				resource := &operatorv1alpha1.Dash0{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &operatorv1alpha1.Dash0{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Dash0")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &Dash0Reconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				dash0 := &operatorv1alpha1.Dash0{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, dash0)).To(Succeed())
				available := meta.FindStatusCondition(dash0.Status.Conditions, string(operatorv1alpha1.ConditionTypeAvailable))
				g.Expect(available).NotTo(BeNil())
				g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
				degraded := meta.FindStatusCondition(dash0.Status.Conditions, string(operatorv1alpha1.ConditionTypeDegraded))
				g.Expect(degraded).To(BeNil())
			}, timeout, pollingInterval).Should(Succeed())
		})
	})
})
