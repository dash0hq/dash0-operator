// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	Context("When reconciling the Dash0 custom resource", func() {

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
			By("Remove all created objects")
			for _, object := range createdObjects {
				Expect(k8sClient.Delete(ctx, object, &client.DeleteOptions{
					GracePeriodSeconds: new(int64),
				})).To(Succeed())
			}

			By("Cleanup the Dash0 custom resource instance")
			if dash0CustomResource := loadDash0CustomResourceIfItExists(ctx); dash0CustomResource != nil {
				// We want to delete the custom resource, but we need to remove the finalizer first, otherwise the first
				// reconcile of the next test case will actually run the finalizers.
				removeFinalizer(ctx, dash0CustomResource)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())
			}

			deleteAllEvents(ctx, clientset, namespace)
			allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(allEvents.Items).To(BeEmpty())
		})

		It("should successfully run the first reconcile (no modifiable workloads exist)", func() {
			By("Trigger reconcile request")
			triggerReconcileRequest(ctx, reconciler, "")
			verifyDash0ResourceIsAvailable(ctx)
		})

		It("should successfully run multiple reconciles (no modifiable workloads exist)", func() {
			triggerReconcileRequest(ctx, reconciler, "First reconcile request")

			firstAvailableStatusCondition := verifyDash0ResourceIsAvailable(ctx)
			originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

			time.Sleep(50 * time.Millisecond)

			triggerReconcileRequest(ctx, reconciler, "Second reconcile request")

			// The LastTransitionTime should not change with subsequent reconciliations.
			secondAvailableCondition := verifyDash0ResourceIsAvailable(ctx)
			Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
		})

		It("should instrument an existing cron job", func() {
			name := CronJobName
			By("Inititalize a cron job")
			cronJob := CreateBasicCronJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, cronJob)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should instrument an existing daemon set", func() {
			name := DaemonSetName
			By("Inititalize a daemon set")
			daemonSet := CreateBasicDaemonSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, daemonSet)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should instrument an existing deployment", func() {
			name := DeploymentName
			By("Inititalize a deployment")
			deployment := CreateBasicDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, deployment)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should record a failure event when attempting to instrument an existing job and add labels", func() {
			name := JobName1
			By("Inititalize a job")
			job := CreateBasicJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, job)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyDash0ResourceIsAvailable(ctx)
			VerifyFailedInstrumentationEvent(
				ctx,
				clientset,
				namespace,
				name,
				"Dash0 instrumentation of this workload by the controller has not been successful. Error message: Dash0 cannot"+
					" instrument the existing job test-namespace/job1, since the this type of workload is immutable.",
			)
			VerifyImmutableJobCouldNotBeModified(GetJob(ctx, k8sClient, namespace, name))
		})

		It("should instrument an existing orphan replicaset", func() {
			name := DeploymentName
			By("Inititalize a replicaset")
			replicaSet := CreateBasicReplicaSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should not modify an existing replicaset owned by a deployment", func() {
			name := DeploymentName
			By("Inititalize a replicaset")
			replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyDash0ResourceIsAvailable(ctx)
			VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
		})

		It("should instrument an stateful set", func() {
			name := StatefulSetName
			By("Inititalize a stateful set")
			statefulSet := CreateBasicStatefulSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, statefulSet)

			triggerReconcileRequest(ctx, reconciler, "")

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should revert an instrumented cron job", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := CronJobName
			By("Create an instrumented cron job")
			cronJob := CreateInstrumentedCronJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, cronJob)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			cronJob = GetCronJob(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedCronJob(cronJob)
			VerifyWebhookIgnoreOnceLabelIsPresent(&cronJob.ObjectMeta)
		})

		It("should revert an instrumented daemon set", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := DaemonSetName
			By("Create an instrumented daemon set")
			daemonSet := CreateInstrumentedDaemonSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, daemonSet)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			daemonSet = GetDaemonSet(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedDaemonSet(daemonSet)
			VerifyWebhookIgnoreOnceLabelIsPresent(&daemonSet.ObjectMeta)
		})

		It("should revert an instrumented deployment", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := DeploymentName
			By("Create an instrumented deployment")
			deployment := CreateInstrumentedDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, deployment)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			deployment = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedDeployment(deployment)
			VerifyWebhookIgnoreOnceLabelIsPresent(&deployment.ObjectMeta)
		})

		It("should record a failure event when attempting to revert an existing instrumenting job (which has been instrumented by the webhook)", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := JobName2
			By("Create an instrumented job")
			job := CreateInstrumentedJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, job)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

			VerifyFailedUninstrumentationEvent(
				ctx,
				clientset,
				namespace,
				name,
				"The controller's attempt to remove the Dash0 instrumentation from this workload has not been successful. Error message: Dash0 cannot remove the instrumentation from the existing job test-namespace/job2, since the this type of workload is immutable.",
			)
			VerifyModifiedJob(GetJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations)
		})

		It("should remove instrumentation labels from an existing uninstrumented job (which has been labelled by the controller without being instrumented)", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := JobName3
			By("Create a job with labels (dash0.instrumented=false)")
			job := CreateJobWithInstrumentationLabels(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, job)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

			VerifyAlreadyNotInstrumented(ctx, clientset, namespace, name, "Dash0 instrumentation was not present on this workload, no modification by the controller has been necessary.")
			VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
		})

		It("should revert an instrumented orphan replica set", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := ReplicaSetName
			By("Create an instrumented replica set")
			replicaSet := CreateInstrumentedReplicaSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			replicaSet = GetReplicaSet(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedReplicaSet(replicaSet)
			VerifyWebhookIgnoreOnceLabelIsPresent(&replicaSet.ObjectMeta)
		})

		It("should leave existing uninstrumented replica sets owned by deployments alone", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := ReplicaSetName
			By("Create an instrumented replica set owned by a deployment")
			replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, replicaSet)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifyNoEvents(ctx, clientset, namespace)
			VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
		})

		It("should revert an instrumented stateful set", func() {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := StatefulSetName
			By("Create an instrumented stateful set")
			statefulSet := CreateInstrumentedStatefulSet(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, statefulSet)

			By("Queue the deletion of the Dash0 custom resource")
			dash0CustomResource := loadDash0CustomResourceOrFail(ctx, Default)
			Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			statefulSet = GetStatefulSet(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedStatefulSet(statefulSet)
			VerifyWebhookIgnoreOnceLabelIsPresent(&statefulSet.ObjectMeta)
		})
	})
})

func triggerReconcileRequest(ctx context.Context, reconciler *Dash0Reconciler, stepMessage string) {
	if stepMessage == "" {
		stepMessage = "Trigger reconcile request"
	}
	By(stepMessage)
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: dash0CustomResourceQualifiedName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func loadDash0CustomResourceOrFail(ctx context.Context, g Gomega) *operatorv1alpha1.Dash0 {
	return loadDash0CustomResource(ctx, g, true)
}

func loadDash0CustomResourceIfItExists(ctx context.Context) *operatorv1alpha1.Dash0 {
	return loadDash0CustomResource(ctx, Default, false)
}

func loadDash0CustomResource(ctx context.Context, g Gomega, failTestsOnNonExists bool) *operatorv1alpha1.Dash0 {
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	if err := k8sClient.Get(ctx, dash0CustomResourceQualifiedName, dash0CustomResource); err != nil {
		if apierrors.IsNotFound(err) {
			if failTestsOnNonExists {
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			} else {
				return nil
			}
		} else {
			// an error occurred, but it is not an IsNotFound error, fail test immediately
			g.Expect(err).NotTo(HaveOccurred())
		}
	}

	return dash0CustomResource
}

func verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx context.Context, namespace string, name string) {
	verifyDash0ResourceIsAvailable(ctx)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
}

func verifyDash0ResourceIsAvailable(ctx context.Context) *metav1.Condition {
	return verifyDash0ResourceStatus(ctx, metav1.ConditionTrue)
}

func verifyDash0ResourceStatus(ctx context.Context, expectedStatus metav1.ConditionStatus) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		dash0CustomResource := loadDash0CustomResourceOrFail(ctx, g)
		availableCondition = meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(util.ConditionTypeAvailable))
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(expectedStatus))
		degraded := meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(util.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func removeFinalizer(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(dash0CustomResource, FinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
	}
}

func deleteAllEvents(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) {
	err := clientset.CoreV1().Events(namespace).DeleteCollection(ctx, metav1.DeleteOptions{
		GracePeriodSeconds: new(int64), // delete immediately
	}, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
}
