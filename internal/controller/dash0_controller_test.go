// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	dash0CustomResourceName = "dash0-test-resource"
)

var (
	dash0CustomResourceQualifiedName = types.NamespacedName{
		Namespace: TestNamespaceName,
		Name:      dash0CustomResourceName,
	}
	namespace = dash0CustomResourceQualifiedName.Namespace

	timeout         = 15 * time.Second
	pollingInterval = 50 * time.Millisecond

	versions = util.Versions{
		OperatorVersion:           "1.2.3",
		InitContainerImageVersion: "4.5.6",
	}
)

var _ = Describe("Dash0 Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()
		var createdObjects []client.Object

		var reconciler *Dash0Reconciler

		BeforeEach(func() {
			By("creating the custom resource for the Kind Dash0")
			EnsureTestNamespaceExists(ctx, k8sClient, namespace)
			EnsureDash0CustomResourceExists(
				ctx,
				k8sClient,
				dash0CustomResourceQualifiedName,
				&operatorv1alpha1.Dash0{},
				&operatorv1alpha1.Dash0{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dash0CustomResourceQualifiedName.Name,
						Namespace: dash0CustomResourceQualifiedName.Namespace,
					},
				},
			)

			reconciler = &Dash0Reconciler{
				Client:    k8sClient,
				ClientSet: clientset,
				Recorder:  recorder,
				Scheme:    k8sClient.Scheme(),
				Versions:  versions,
			}
			createdObjects = make([]client.Object, 0)
		})

		AfterEach(func() {
			By("Cleanup the Dash0 resource instance")
			dash0CustomResource := &operatorv1alpha1.Dash0{}
			err := k8sClient.Get(ctx, dash0CustomResourceQualifiedName, dash0CustomResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			for _, object := range createdObjects {
				Expect(k8sClient.Delete(ctx, object, &client.DeleteOptions{
					GracePeriodSeconds: new(int64),
				})).To(Succeed())
			}

			DeleteAllEvents(ctx, clientset, namespace)
			allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(allEvents.Items).To(BeEmpty())
		})

		It("should successfully run the first reconcile (no modifiable resources exist)", func() {
			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
		})

		It("should successfully run multiple reconciles (no modifiable resources exist)", func() {
			By("First reconcile request")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			firstAvailableStatusCondition := verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
			originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

			time.Sleep(50 * time.Millisecond)

			By("Second reconcile request")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// The LastTransitionTime should not change with subsequent reconciliations.
			secondAvailableCondition := verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
			Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
		})

		It("should modify an existing cron job", func() {
			name := CronJobName
			By("Inititalize a cron job")
			cronJob := CreateBasicCronJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, cronJob)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditionAndSuccessEvent(ctx, namespace, name)
			VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, namespace, name), BasicPodSpecExpectations)
		})

		It("should modify an existing daemon set", func() {
			name := DaemonSetName
			By("Inititalize a daemon set")
			daemonSet := CreateBasicDaemonSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, daemonSet)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditionAndSuccessEvent(ctx, namespace, name)
			VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, namespace, name), BasicPodSpecExpectations)
		})

		It("should modify an existing deployment", func() {
			name := DeploymentName
			By("Inititalize a deployment")
			deployment := CreateBasicDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, deployment)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditionAndSuccessEvent(ctx, namespace, name)
			VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, name), BasicPodSpecExpectations)
		})

		It("should record a failure event for an existing job and add labels", func() {
			name := JobName
			By("Inititalize a job")
			job := CreateBasicJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, job)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
			VerifyFailureEvent(
				ctx,
				clientset,
				namespace,
				name,
				"controller",
				"Dash0 instrumentation by controller has not been successful. Error message: Dash0 cannot"+
					" instrument the existing job test-namespace/job, since the this type of resource is immutable.",
			)
			VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
		})

		It("should modify an existing orphan replicaset", func() {
			name := DeploymentName
			By("Inititalize a replicaset")
			replicaSet := CreateBasicReplicaSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditionAndSuccessEvent(ctx, namespace, name)
			VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name), BasicPodSpecExpectations)
		})

		It("should not modify an existing replicaset with an owner", func() {
			name := DeploymentName
			By("Inititalize a replicaset")
			replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
			VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
		})

		It("should modify an stateful set", func() {
			name := StatefulSetName
			By("Inititalize a stateful set")
			statefulSet := CreateBasicStatefulSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, statefulSet)

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: dash0CustomResourceQualifiedName,
			})
			Expect(err).NotTo(HaveOccurred())

			verifyStatusConditionAndSuccessEvent(ctx, namespace, name)
			VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, namespace, name), BasicPodSpecExpectations)
		})
	})
})

func verifyStatusConditionAndSuccessEvent(ctx context.Context, namespace string, name string) {
	verifyStatusConditions(ctx, dash0CustomResourceQualifiedName)
	VerifySuccessEvent(ctx, clientset, namespace, name, "controller")
}

func verifyStatusConditions(ctx context.Context, typeNamespacedName types.NamespacedName) *metav1.Condition {
	var available *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		dash0 := &operatorv1alpha1.Dash0{}
		g.Expect(k8sClient.Get(ctx, typeNamespacedName, dash0)).To(Succeed())
		available = meta.FindStatusCondition(dash0.Status.Conditions, string(util.ConditionTypeAvailable))
		g.Expect(available).NotTo(BeNil())
		g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(dash0.Status.Conditions, string(util.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return available
}
