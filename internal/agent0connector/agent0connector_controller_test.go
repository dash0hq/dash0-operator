// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

func serviceAccount(namespace, name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

var _ = Describe("The agent0-connector controller", func() {
	Describe("resourceMatches", func() {
		It("matches when the namespace and name match", func() {
			Expect(resourceMatches(serviceAccount("ns", "a"), "ns", []string{"a", "b"})).To(BeTrue())
		})

		It("does not match when the name is not in the list", func() {
			Expect(resourceMatches(serviceAccount("ns", "c"), "ns", []string{"a", "b"})).To(BeFalse())
		})

		It("does not match when the namespace differs", func() {
			Expect(resourceMatches(serviceAccount("other", "a"), "ns", []string{"a"})).To(BeFalse())
		})

		It("matches a cluster-scoped resource (empty namespace) against an empty target namespace", func() {
			clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "a"}}
			Expect(resourceMatches(clusterRole, "", []string{"a"})).To(BeTrue())
		})
	})

	Describe("createNameFilterPredicate", func() {
		const namePrefix = "dash0-operator-test"
		reconciler := NewAgent0ConnectorReconciler(nil, nil, OperatorNamespace, namePrefix)

		Describe("for a namespaced resource", func() {
			predicate := reconciler.createNameFilterPredicate([]string{"watched"}, true)

			It("accepts a create event for the watched name in the operator namespace", func() {
				Expect(predicate.Create(event.CreateEvent{Object: serviceAccount(OperatorNamespace, "watched")})).To(BeTrue())
			})

			It("rejects a create event for the watched name in a different namespace", func() {
				Expect(predicate.Create(event.CreateEvent{Object: serviceAccount("other", "watched")})).To(BeFalse())
			})

			It("rejects a create event for an unwatched name", func() {
				Expect(predicate.Create(event.CreateEvent{Object: serviceAccount(OperatorNamespace, "other")})).To(BeFalse())
			})

			It("accepts an update event when either the old or the new object matches", func() {
				matching := serviceAccount(OperatorNamespace, "watched")
				other := serviceAccount(OperatorNamespace, "other")
				Expect(predicate.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: matching})).To(BeTrue())
				Expect(predicate.Update(event.UpdateEvent{ObjectOld: matching, ObjectNew: other})).To(BeTrue())
				Expect(predicate.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: other})).To(BeFalse())
			})

			It("accepts a delete event for the watched name", func() {
				Expect(predicate.Delete(event.DeleteEvent{Object: serviceAccount(OperatorNamespace, "watched")})).To(BeTrue())
			})
		})

		Describe("for a cluster-scoped resource", func() {
			predicate := reconciler.createNameFilterPredicate([]string{"watched"}, false)

			It("accepts a create event for the watched name with no namespace", func() {
				clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "watched"}}
				Expect(predicate.Create(event.CreateEvent{Object: clusterRole})).To(BeTrue())
			})

			It("rejects a create event for a namespaced object even if the name matches", func() {
				Expect(predicate.Create(event.CreateEvent{Object: serviceAccount(OperatorNamespace, "watched")})).To(BeFalse())
			})
		})
	})
})
