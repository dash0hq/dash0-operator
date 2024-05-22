// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/k8sresources"
)

var (
	timeout         = 15 * time.Second
	pollingInterval = 50 * time.Millisecond

	versions = k8sresources.Versions{
		OperatorVersion:           "1.2.3",
		InitContainerImageVersion: "4.5.6",
	}
)

var _ = Describe("Dash0 Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "dash0-test-resource"

		ctx := context.Background()

		dash0ResourceName := types.NamespacedName{
			Namespace: TestNamespaceName,
			Name:      resourceName,
		}
		namespace := dash0ResourceName.Namespace
		var reconciler *Dash0Reconciler

		BeforeEach(func() {
			By("creating the custom resource for the Kind Dash0", func() {
				EnsureTestNamespaceExists(ctx, k8sClient, namespace)
				EnsureResourceExists(
					ctx,
					k8sClient,
					dash0ResourceName,
					&operatorv1alpha1.Dash0{},
					&operatorv1alpha1.Dash0{
						ObjectMeta: metav1.ObjectMeta{
							Name:      dash0ResourceName.Name,
							Namespace: dash0ResourceName.Namespace,
						},
					},
				)
			})

			reconciler = &Dash0Reconciler{
				Client:    k8sClient,
				ClientSet: clientset,
				Recorder:  recorder,
				Scheme:    k8sClient.Scheme(),
				Versions:  versions,
			}
		})

		AfterEach(func() {
			By("Cleanup the Dash0 resource instance", func() {
				resource := &operatorv1alpha1.Dash0{}
				err := k8sClient.Get(ctx, dash0ResourceName, resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})
		})

		It("should successfully run the first reconcile (no modifiable resources exist)", func() {
			By("Reconciling the created resource", func() {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: dash0ResourceName,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			verifyStatusConditions(ctx, dash0ResourceName)
		})

		It("should successfully run multiple reconciles (no modifiable resources exist)", func() {
			By("First reconcile request", func() {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: dash0ResourceName,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			firstAvailableStatusCondition := verifyStatusConditions(ctx, dash0ResourceName)
			originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

			time.Sleep(50 * time.Millisecond)

			By("Second reconcile request", func() {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: dash0ResourceName,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			// The LastTransitionTime should not change with subsequent reconciliations.
			secondAvailableCondition := verifyStatusConditions(ctx, dash0ResourceName)
			Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
		})

		It("should modify an existing deployment", func() {
			deploymentName := DeploymentNameExisting
			By("Inititalize a deployment", func() {
				CreateBasicDeployment(ctx, k8sClient, namespace, deploymentName)
			})

			By("Reconciling the created resource", func() {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: dash0ResourceName,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			verifyStatusConditions(ctx, dash0ResourceName)
			VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, deploymentName), DeploymentExpectations{
				Volumes:               1,
				Dash0VolumeIdx:        0,
				InitContainers:        1,
				Dash0InitContainerIdx: 0,
				Containers: []ContainerExpectations{{
					VolumeMounts:                             1,
					Dash0VolumeMountIdx:                      0,
					EnvVars:                                  2,
					NodeOptionsEnvVarIdx:                     0,
					Dash0CollectorBaseUrlEnvVarIdx:           1,
					Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
				}},
			})
			VerifySuccessEvent(ctx, clientset, namespace, deploymentName, "controller")
		})
	})
})

func verifyStatusConditions(ctx context.Context, typeNamespacedName types.NamespacedName) *metav1.Condition {
	var available *metav1.Condition
	By("Verifying status conditions", func() {
		Eventually(func(g Gomega) {
			dash0 := &operatorv1alpha1.Dash0{}
			g.Expect(k8sClient.Get(ctx, typeNamespacedName, dash0)).To(Succeed())
			available = meta.FindStatusCondition(dash0.Status.Conditions, string(operatorv1alpha1.ConditionTypeAvailable))
			g.Expect(available).NotTo(BeNil())
			g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
			degraded := meta.FindStatusCondition(dash0.Status.Conditions, string(operatorv1alpha1.ConditionTypeDegraded))
			g.Expect(degraded).To(BeNil())
		}, timeout, pollingInterval).Should(Succeed())
	})
	return available
}
