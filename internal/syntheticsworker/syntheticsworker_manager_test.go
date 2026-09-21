// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package syntheticsworker

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const syntheticsWorkerTestNamePrefix = "dash0-operator-test"

func newResourceManager() *swresources.SyntheticsWorkerResourceManager {
	return swresources.NewSyntheticsWorkerResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.SyntheticsWorkerConfig{
			Images:            util.Images{SyntheticsWorkerImage: "ghcr.io/dash0hq/dash0-synthetics-worker:test"},
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        syntheticsWorkerTestNamePrefix,
			ServerAddress:     SyntheticsWorkerServerAddress,
		},
	)
}

// eventRecorder is set by newManager, so that a test can assert which events the manager under test has queued.
var eventRecorder *events.FakeRecorder

func newManager() *SyntheticsWorkerManager {
	eventRecorder = events.NewFakeRecorder(10)
	return NewSyntheticsWorkerManager(k8sClient, false, newResourceManager(), eventRecorder)
}

// recordedEvents drains the events the manager under test has queued so far.
func recordedEvents() []string {
	var recorded []string
	for {
		select {
		case event := <-eventRecorder.Events:
			recorded = append(recorded, event)
		default:
			return recorded
		}
	}
}

func expectSyntheticsWorkerStatus(ctx context.Context) *dash0v1alpha1.SyntheticsWorkerStatus {
	GinkgoHelper()
	operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
	Expect(operatorConfigurationResource.Status.SyntheticsWorker).ToNot(BeNil())
	return operatorConfigurationResource.Status.SyntheticsWorker
}

var _ = Describe("The synthetics-worker failure reason", func() {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "apps", Resource: "deployments"},
		"dash0-operator-synthetics-worker",
		errors.New("user \"system:serviceaccount:dash0-system:dash0-operator-sa\" (groups=[]) is attempting to grant "+
			"RBAC permissions not currently held"),
	)

	DescribeTable(
		"maps a reconcile error to the identifier reported in the status",
		func(err error, expectedReason string) {
			Expect(syntheticsWorkerFailureReason(err)).To(Equal(expectedReason))
		},
		Entry("missing location ID", swresources.ErrNoLocationID, StatusReasonNoLocationID),
		Entry("missing authorization token", swresources.ErrNoAuthorizationToken, StatusReasonNoAuthorizationToken),
		Entry("the API server rejecting the deployment", forbidden, StatusReasonOperatorMissingPermissions),
		// The error travels through the resource manager, so the check has to survive wrapping.
		Entry("a wrapped rejection", fmt.Errorf("cannot create the deployment: %w", forbidden),
			StatusReasonOperatorMissingPermissions),
		Entry("any other error", errors.New("connection refused"), StatusReasonReconcileFailed),
	)

	It("maps forbidden", func() {
		message := syntheticsWorkerFailureMessage(StatusReasonOperatorMissingPermissions, forbidden)
		Expect(message).To(ContainSubstring("privilege escalation prevention"))
		Expect(message).To(ContainSubstring(forbidden.Error()))
		Expect(message).ToNot(Equal(forbidden.Error()))
	})

	It("reports any other failure with the error itself", func() {
		err := errors.New("connection refused")
		Expect(syntheticsWorkerFailureMessage(StatusReasonReconcileFailed, err)).To(Equal("connection refused"))
	})
})

var _ = Describe("The synthetics-worker manager", Ordered, func() {
	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		_, err := newResourceManager().DeleteResources(ctx, logd.FromContext(ctx))
		Expect(err).ToNot(HaveOccurred())
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("creates the synthetics-worker resources when enabled and configured", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithSyntheticsWorker(nil))

		hasBeenReconciled, err := newManager().ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectSyntheticsWorkerResourcesToExist(ctx)
		Expect(expectSyntheticsWorkerStatus(ctx).Deployed).To(BeTrue())
	})

	It("removes the synthetics-worker resources when it is disabled in the operator configuration resource", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithSyntheticsWorker(nil))
		manager := newManager()
		_, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectSyntheticsWorkerResourcesToExist(ctx)

		disableSyntheticsWorkerInOperatorConfigurationResource(ctx)
		_ = recordedEvents()
		hasBeenReconciled, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectSyntheticsWorkerResourcesToNotExist(ctx)
		status := expectSyntheticsWorkerStatus(ctx)
		Expect(status.Deployed).To(BeFalse())
		Expect(status.Reason).To(Equal(StatusReasonDisabled))
		Expect(status.Message).To(ContainSubstring("disabled in the Dash0 operator configuration resource"))
		Expect(recordedEvents()).To(ContainElement(ContainSubstring("SyntheticsWorkerDisabled")))
	})

	It("reports the synthetics-worker as disabled only once while it stays disabled", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithSyntheticsWorker(ptr.To(false)))
		manager := newManager()
		_, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(recordedEvents()).To(ContainElement(ContainSubstring("SyntheticsWorkerDisabled")))

		// Reconciling again must not queue the event a second time, the outcome has not changed.
		_, err = manager.ReconcileSyntheticsWorker(ctx, TriggeredByWatchEvent)

		Expect(err).ToNot(HaveOccurred())
		Expect(recordedEvents()).To(BeEmpty())
	})

	It("removes the synthetics-worker resources when there is no operator configuration resource", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithSyntheticsWorker(nil))
		_, err := newManager().ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectSyntheticsWorkerResourcesToExist(ctx)

		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		hasBeenReconciled, err := newManager().ReconcileSyntheticsWorker(ctx, TriggeredByWatchEvent)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectSyntheticsWorkerResourcesToNotExist(ctx)
	})

	It("does not report an error when the synthetics-worker is misconfigured", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithMissingLocationID())
		manager := newManager()

		hasBeenReconciled, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		// Reporting an error would make the caller requeue the reconcile request, which cannot fix a missing
		// location ID (it requires a user to edit the resource), and it would abort the caller's remaining
		// reconciliation steps.
		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeFalse())
		expectSyntheticsWorkerResourcesToNotExist(ctx)
	})

	It("reports a misconfiguration in the status and queues a warning event", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithMissingLocationID())
		manager := newManager()

		_, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())

		status := expectSyntheticsWorkerStatus(ctx)
		Expect(status.Deployed).To(BeFalse())
		Expect(status.Reason).To(Equal(StatusReasonNoLocationID))
		Expect(status.LastTransitionTime).ToNot(BeZero())

		Expect(recordedEvents()).To(ConsistOf(ContainSubstring("SyntheticsWorkerNotDeployed")))
	})

	It("keeps the operator configuration resource available while the synthetics-worker is misconfigured", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithMissingLocationID())
		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

		manager := newManager()
		_, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())

		// The synthetics-worker is an optional feature, an issue with it must not make the operator configuration
		// resource unavailable or degraded.
		operatorConfigurationResource = LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		Expect(operatorConfigurationResource.IsAvailable()).To(BeTrue())
		Expect(operatorConfigurationResource.IsDegraded()).To(BeFalse())
		Expect(operatorConfigurationResource.Status.SyntheticsWorker.Deployed).To(BeFalse())
	})

	It("reports the recovery in the status and queues a normal event", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithMissingLocationID())
		manager := newManager()
		_, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(expectSyntheticsWorkerStatus(ctx).Deployed).To(BeFalse())
		Expect(recordedEvents()).To(HaveLen(1))

		// The operator of the cluster fixes the resource by adding a location ID.
		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		operatorConfigurationResource.Spec.SyntheticsWorker.LocationID = "test-location"
		Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())

		hasBeenReconciled, err := manager.ReconcileSyntheticsWorker(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())

		status := expectSyntheticsWorkerStatus(ctx)
		Expect(status.Deployed).To(BeTrue())
		Expect(status.Reason).To(Equal(StatusReasonDeployed))
		Expect(recordedEvents()).To(ConsistOf(ContainSubstring("SyntheticsWorkerDeployed")))
		expectSyntheticsWorkerResourcesToExist(ctx)
	})

	It("does not reconcile when a reconciliation is already in progress, but does not lose the trigger", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithSyntheticsWorker(nil))
		manager := newManager()

		// Occupy the manager's reconcile guard and trigger a reconciliation from within it, the way a watch event
		// would arrive while a reconciliation is running.
		executions := 0
		var skippedHasBeenReconciled bool
		var skippedErr error
		_, err := manager.reconcileGuard.Run(func() (bool, error) {
			executions++
			if executions == 1 {
				skippedHasBeenReconciled, skippedErr =
					manager.ReconcileSyntheticsWorker(ctx, TriggeredByWatchEvent)
			}
			return true, nil
		}, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(skippedErr).ToNot(HaveOccurred())
		Expect(skippedHasBeenReconciled).To(BeFalse())
		// The reconciliation was not executed, so no resources were created ...
		expectSyntheticsWorkerResourcesToNotExist(ctx)
		// ... but the trigger was recorded and the guard repeated the reconciliation once, instead of dropping it.
		Expect(executions).To(Equal(2))
	})
})

// operatorConfigurationSpecWithSyntheticsWorker returns the default operator configuration spec with
// spec.syntheticsWorker configured with a location ID, a literal authorization token, and the given explicit value
// for enabled (nil leaves it unset, following the Helm-level default).
func operatorConfigurationSpecWithSyntheticsWorker(enabled *bool) dash0v1alpha1.Dash0OperatorConfigurationSpec {
	spec := OperatorConfigurationResourceDefaultSpec
	token := "synthetics-worker-test-token"
	spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
		Enabled:       enabled,
		LocationID:    "test-location",
		Authorization: &dash0common.Authorization{Token: &token},
	}
	return spec
}

// operatorConfigurationSpecWithMissingLocationID returns the default operator configuration spec with
// spec.syntheticsWorker configured with an authorization token but without a location ID.
func operatorConfigurationSpecWithMissingLocationID() dash0v1alpha1.Dash0OperatorConfigurationSpec {
	spec := OperatorConfigurationResourceDefaultSpec
	token := "synthetics-worker-test-token"
	spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
		Authorization: &dash0common.Authorization{Token: &token},
	}
	return spec
}

// disableSyntheticsWorkerInOperatorConfigurationResource sets spec.syntheticsWorker.enabled to false on the operator
// configuration resource in the cluster, which is how a user opts out of the synthetics-worker.
func disableSyntheticsWorkerInOperatorConfigurationResource(ctx context.Context) {
	GinkgoHelper()
	operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
	operatorConfigurationResource.Spec.SyntheticsWorker.Enabled = ptr.To(false)
	Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
}

func expectSyntheticsWorkerResourcesToExist(ctx context.Context) {
	GinkgoHelper()
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: swresources.ServiceAccountName(syntheticsWorkerTestNamePrefix)},
		&corev1.ServiceAccount{})).To(Succeed())
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: swresources.DeploymentName(syntheticsWorkerTestNamePrefix)},
		&appsv1.Deployment{})).To(Succeed())
}

func expectSyntheticsWorkerResourcesToNotExist(ctx context.Context) {
	GinkgoHelper()
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: swresources.ServiceAccountName(syntheticsWorkerTestNamePrefix)},
		&corev1.ServiceAccount{}))).To(BeTrue())
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: swresources.DeploymentName(syntheticsWorkerTestNamePrefix)},
		&appsv1.Deployment{}))).To(BeTrue())
}
