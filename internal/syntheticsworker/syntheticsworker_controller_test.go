// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package syntheticsworker

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"
	. "github.com/dash0hq/dash0-operator/test/util"
)

func serviceAccount(namespace, name string, labels map[string]string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels}}
}

var _ = Describe("The synthetics-worker controller", func() {
	featureLabels := swresources.FeatureLabelSelector()
	unrelatedLabels := map[string]string{"app.kubernetes.io/name": "something-else"}

	Describe("resourceMatches", func() {
		It("matches when the namespace matches and the object carries the synthetics-worker feature label", func() {
			Expect(resourceMatches(serviceAccount("ns", "a", featureLabels), "ns")).To(BeTrue())
		})

		It("does not match when the object lacks the synthetics-worker feature label", func() {
			Expect(resourceMatches(serviceAccount("ns", "a", unrelatedLabels), "ns")).To(BeFalse())
		})

		It("does not match when the namespace differs", func() {
			Expect(resourceMatches(serviceAccount("other", "a", featureLabels), "ns")).To(BeFalse())
		})
	})

	Describe("createFeatureFilterPredicate", func() {
		const namePrefix = "dash0-operator-test"
		reconciler := NewSyntheticsWorkerReconciler(nil, nil, OperatorNamespace, namePrefix)
		predicate := reconciler.createFeatureFilterPredicate()

		It("accepts a create event for a synthetics-worker resource in the operator namespace", func() {
			Expect(predicate.Create(event.CreateEvent{Object: serviceAccount(OperatorNamespace, "watched", featureLabels)})).To(BeTrue())
		})

		It("rejects a create event for a synthetics-worker resource in a different namespace", func() {
			Expect(predicate.Create(event.CreateEvent{Object: serviceAccount("other", "watched", featureLabels)})).To(BeFalse())
		})

		It("rejects a create event for an unrelated resource", func() {
			Expect(predicate.Create(event.CreateEvent{Object: serviceAccount(OperatorNamespace, "other", unrelatedLabels)})).To(BeFalse())
		})

		It("accepts an update event when either the old or the new object matches", func() {
			matching := serviceAccount(OperatorNamespace, "watched", featureLabels)
			other := serviceAccount(OperatorNamespace, "other", unrelatedLabels)
			Expect(predicate.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: matching})).To(BeTrue())
			Expect(predicate.Update(event.UpdateEvent{ObjectOld: matching, ObjectNew: other})).To(BeTrue())
			Expect(predicate.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: other})).To(BeFalse())
		})

		It("accepts a delete event for a synthetics-worker resource", func() {
			Expect(predicate.Delete(event.DeleteEvent{Object: serviceAccount(OperatorNamespace, "watched", featureLabels)})).To(BeTrue())
		})
	})
})
