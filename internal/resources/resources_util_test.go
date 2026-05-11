// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type statusExpectation struct {
	status  metav1.ConditionStatus
	reason  string
	message string
}

type statusExpectations struct {
	available *statusExpectation
	degraded  *statusExpectation
}

type verifyThatResourceIsUniqueInScopeTest struct {
	createResources          func() *dash0v1beta1.Dash0Monitoring
	expectedStopReconcile    bool
	expectedStatusConditions map[types.NamespacedName]statusExpectations
}

const (
	timeout         = 1 * time.Second
	pollingInterval = 50 * time.Millisecond

	statusConditionReason     = "TestReason"
	statusConditionMessage    = "This is a test message."
	updateStatusFailedMessage = "failed to update status"
)

var (
	resourceName1 = types.NamespacedName{Namespace: TestNamespaceName, Name: "resource1"}
	resourceName2 = types.NamespacedName{Namespace: TestNamespaceName, Name: "resource2"}
	resourceName3 = types.NamespacedName{Namespace: TestNamespaceName, Name: "resource3"}

	reconcileRequest = ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: TestNamespaceName,
		},
	}

	resourceIsAvailable = statusExpectations{
		available: &statusExpectation{
			status:  metav1.ConditionTrue,
			reason:  "ReconcileFinished",
			message: "Dash0 monitoring is active in this namespace now.",
		},
		degraded: nil,
	}

	resourceIsDegradedTestReason = statusExpectations{
		available: &statusExpectation{
			status:  metav1.ConditionFalse,
			reason:  statusConditionReason,
			message: statusConditionMessage,
		},
		degraded: &statusExpectation{
			status:  metav1.ConditionTrue,
			reason:  statusConditionReason,
			message: statusConditionMessage,
		},
	}

	resourceIsDegradedNewerResourcePresent = statusExpectations{
		available: &statusExpectation{
			status: metav1.ConditionFalse,
			reason: "NewerResourceIsPresent",
			message: "There is a more recently created Dash0 monitoring resource in this namespace, please remove " +
				"all but one resource instance.",
		},
		degraded: &statusExpectation{
			status: metav1.ConditionTrue,
			reason: "NewerResourceIsPresent",
			message: "There is a more recently created Dash0 monitoring resource in this namespace, please remove " +
				"all but one resource instance.",
		},
	}
)

var _ = Describe("resource util functions", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var createdObjectsResourcesUtilTest []client.Object

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjectsResourcesUtilTest = make([]client.Object, 0)
	})

	AfterEach(func() {
		// Some tests mark monitoring resources for deletion by adding a finalizer and calling Delete. Strip those
		// finalizers here; once the finalizer is gone the API server reaps the already-deleting resource, so the
		// subsequent Delete call below must tolerate NotFound.
		for _, obj := range createdObjectsResourcesUtilTest {
			m, ok := obj.(*dash0v1beta1.Dash0Monitoring)
			if !ok {
				continue
			}
			latest := &dash0v1beta1.Dash0Monitoring{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: m.Namespace, Name: m.Name}, latest); err != nil {
				continue
			}
			if controllerutil.RemoveFinalizer(latest, dash0common.MonitoringFinalizerId) {
				Expect(k8sClient.Update(ctx, latest)).To(Succeed())
			}
		}
		for _, obj := range createdObjectsResourcesUtilTest {
			err := k8sClient.Delete(ctx, obj, &client.DeleteOptions{GracePeriodSeconds: new(int64)})
			if err != nil && !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}
		createdObjectsResourcesUtilTest = make([]client.Object, 0)
	})

	markMonitoringResourceForDeletion := func(resource *dash0v1beta1.Dash0Monitoring) *dash0v1beta1.Dash0Monitoring {
		controllerutil.AddFinalizer(resource, dash0common.MonitoringFinalizerId)
		Expect(k8sClient.Update(ctx, resource)).To(Succeed())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		name := types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}
		Expect(k8sClient.Get(ctx, name, resource)).To(Succeed())
		Expect(resource.IsMarkedForDeletion()).To(BeTrue())
		return resource
	}

	DescribeTable("VerifyThatResourceIsUniqueInScope", func(testConfig verifyThatResourceIsUniqueInScopeTest) {
		reconciledResource := testConfig.createResources()

		stopRecconile, err := VerifyThatResourceIsUniqueInScope(
			ctx,
			k8sClient,
			reconcileRequest,
			reconciledResource,
			updateStatusFailedMessage,
			logger,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(stopRecconile).To(Equal(testConfig.expectedStopReconcile))

		Eventually(func(g Gomega) {
			for resName, expectations := range testConfig.expectedStatusConditions {
				availableCondition := LoadMonitoringResourceStatusCondition(ctx, k8sClient, resName, dash0common.ConditionTypeAvailable)
				verifyResourceStatusCondition(
					g,
					availableCondition,
					expectations.available,
				)
				degradedCondition := LoadMonitoringResourceStatusCondition(ctx, k8sClient, resName, dash0common.ConditionTypeDegraded)
				verifyResourceStatusCondition(
					g,
					degradedCondition,
					expectations.degraded,
				)
			}
		}, timeout, pollingInterval).Should(Succeed())
	},
		Entry("no resources exist", verifyThatResourceIsUniqueInScopeTest{
			createResources:       DefaultMonitoringResource,
			expectedStopReconcile: false,
		}),
		Entry("one degraded resource", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource)
				resource.EnsureResourceIsMarkedAsDegraded(statusConditionReason, statusConditionMessage)
				Expect(k8sClient.Status().Update(ctx, resource)).To(Succeed())
				return resource
			},
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				resourceName1: resourceIsDegradedTestReason,
			},
		}),
		Entry("one available resource", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource)
				resource.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource)).To(Succeed())
				return resource
			},
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				resourceName1: resourceIsAvailable,
			},
		}),
		Entry("multiple resources, the reconciled resource is the most recent one and it is degraded", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource1 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource1)
				resource1.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource1)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource2 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName2)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource2)
				resource2.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource2)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource3 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName3)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource3)
				resource3.EnsureResourceIsMarkedAsDegraded(statusConditionReason, statusConditionMessage)
				Expect(k8sClient.Status().Update(ctx, resource3)).To(Succeed())

				// simulate a reconcile request for resource3, the most recent resource
				return resource3
			},
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName1: resourceIsDegradedNewerResourcePresent,
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName2: resourceIsDegradedNewerResourcePresent,
				// The most recent one, but it was degraded before, we do not expect VerifyThatResourceIsUniqueInScope
				// to set its status to available, this is done by code calling VerifyThatResourceIsUniqueInScope
				resourceName3: resourceIsDegradedTestReason,
			},
		}),
		Entry("multiple resources, the reconciled resource is the most recent one and it is available", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource1 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource1)
				resource1.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource1)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource2 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName2)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource2)
				resource2.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource2)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource3 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName3)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource3)
				resource3.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource3)).To(Succeed())

				// simulate a reconcile request for resource3, the most recent resource
				return resource3
			},
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName1: resourceIsDegradedNewerResourcePresent,
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName2: resourceIsDegradedNewerResourcePresent,
				// The most recent one, but it was degraded before, we do not expect VerifyThatResourceIsUniqueInScope
				// to set its status to available, this is done by code calling VerifyThatResourceIsUniqueInScope.
				resourceName3: resourceIsAvailable,
			},
		}),
		Entry("multiple resources, the reconciled resource is not the ost recent one", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource1 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource1)
				resource1.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource1)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource2 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName2)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource2)
				resource2.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource2)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource3 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName3)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource3)
				resource3.EnsureResourceIsMarkedAsDegraded(statusConditionReason, statusConditionMessage)
				Expect(k8sClient.Status().Update(ctx, resource3)).To(Succeed())

				// simulate a reconcile request for resource2, which is not the most recent one
				return resource2
			},
			expectedStopReconcile: true,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName1: resourceIsDegradedNewerResourcePresent,
				// Not the most recent one, we expect VerifyThatResourceIsUniqueInScope to switch its status to degraded
				resourceName2: resourceIsDegradedNewerResourcePresent,
				// The most recent one, but it was degraded before, we do not expect VerifyThatResourceIsUniqueInScope
				// to set its status to available, this is done by code calling VerifyThatResourceIsUniqueInScope.
				resourceName3: resourceIsDegradedTestReason,
			},
		}),
		// Regression test for an index-out-of-range panic: when the reconciled resource was marked for deletion and
		// other resources existed in the namespace, the filter loop excluded every resource and the subsequent
		// most-recent lookup indexed into an empty slice.
		Entry("the reconciled resource is marked for deletion", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource1 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource1)
				resource1.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource1)).To(Succeed())
				time.Sleep(10 * time.Millisecond)

				resource2 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName2)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource2)
				resource2.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource2)).To(Succeed())

				// simulate a reconcile request for resource2 after it has been marked for deletion
				return markMonitoringResourceForDeletion(resource2)
			},
			// After excluding the deletion-marked reconciled resource, only resource1 remains in scope, so the
			// uniqueness check returns without flipping any status.
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				resourceName1: resourceIsAvailable,
			},
		}),
		Entry("multiple resources exist but only one is not marked for deletion", verifyThatResourceIsUniqueInScopeTest{
			createResources: func() *dash0v1beta1.Dash0Monitoring {
				resource1 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName1)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource1)
				resource1.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource1)).To(Succeed())
				markMonitoringResourceForDeletion(resource1)
				time.Sleep(10 * time.Millisecond)

				resource2 := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName2)
				createdObjectsResourcesUtilTest = append(createdObjectsResourcesUtilTest, resource2)
				resource2.EnsureResourceIsMarkedAsAvailable()
				Expect(k8sClient.Status().Update(ctx, resource2)).To(Succeed())

				// simulate a reconcile request for resource2, the only live resource
				return resource2
			},
			expectedStopReconcile: false,
			expectedStatusConditions: map[types.NamespacedName]statusExpectations{
				// The deletion-marked sibling must not be flipped to degraded by the uniqueness check.
				resourceName1: resourceIsAvailable,
				resourceName2: resourceIsAvailable,
			},
		}),
	)
})

func verifyResourceStatusCondition(g Gomega, condition *metav1.Condition, expectation *statusExpectation) {
	if expectation == nil {
		g.Expect(condition).To(BeNil())
	} else {
		VerifyResourceStatusCondition(
			g,
			condition,
			expectation.status,
			expectation.reason,
			expectation.message,
		)
	}
}
