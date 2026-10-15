// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

var _ = Describe("The generation, label or dash0.com/ annotation change predicate", func() {
	DescribeTable("reacts on update events",
		func(oldAnnotations map[string]string, newAnnotations map[string]string, expected bool) {
			oldView := &dash0v1alpha1.Dash0View{
				ObjectMeta: metav1.ObjectMeta{Name: "view", Generation: 1, Annotations: oldAnnotations},
			}
			newView := &dash0v1alpha1.Dash0View{
				ObjectMeta: metav1.ObjectMeta{Name: "view", Generation: 1, Annotations: newAnnotations},
			}
			Expect(generationLabelOrDash0AnnotationChangePredicate.Update(
				event.UpdateEvent{ObjectOld: oldView, ObjectNew: newView},
			)).To(Equal(expected))
		},
		Entry("no annotations", nil, nil, false),
		Entry("unchanged dash0.com/ annotation",
			map[string]string{"dash0.com/folder-path": "/a"},
			map[string]string{"dash0.com/folder-path": "/a"},
			false,
		),
		Entry("changed dash0.com/ annotation",
			map[string]string{"dash0.com/folder-path": "/a"},
			map[string]string{"dash0.com/folder-path": "/b"},
			true,
		),
		Entry("added dash0.com/ annotation",
			nil,
			map[string]string{"dash0.com/sharing": "team:team_01abc"},
			true,
		),
		Entry("removed dash0.com/ annotation",
			map[string]string{"dash0.com/folder-path": "/a", "other": "x"},
			map[string]string{"other": "x"},
			true,
		),
		Entry("changed foreign annotation",
			map[string]string{"dash0.com/folder-path": "/a", "other": "x"},
			map[string]string{"dash0.com/folder-path": "/a", "other": "y"},
			false,
		),
	)

	It("still reacts on generation changes", func() {
		oldView := &dash0v1alpha1.Dash0View{ObjectMeta: metav1.ObjectMeta{Name: "view", Generation: 1}}
		newView := &dash0v1alpha1.Dash0View{ObjectMeta: metav1.ObjectMeta{Name: "view", Generation: 2}}
		Expect(generationLabelOrDash0AnnotationChangePredicate.Update(
			event.UpdateEvent{ObjectOld: oldView, ObjectNew: newView},
		)).To(BeTrue())
	})
})
