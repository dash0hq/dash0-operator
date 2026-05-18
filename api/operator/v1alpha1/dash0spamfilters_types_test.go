// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha2 "github.com/dash0hq/dash0-operator/api/operator/v1alpha2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("v1alpha1 Dash0 spam filter CRD", func() {

	Describe("converting to and from hub version", func() {

		It("should convert v1alpha1 to v1alpha2 (contexts → context)", func() {
			src := &Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-spam-filter",
				},
				Spec: Dash0SpamFilterSpec{
					Contexts: []string{"log"},
					Filter: []Dash0SpamFilterCondition{
						{
							Key:      "k8s.namespace.name",
							Operator: "is",
							Value:    ptr.To("kube-system"),
						},
					},
				},
			}

			dst := &dash0v1alpha2.Dash0SpamFilter{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.ObjectMeta).To(Equal(src.ObjectMeta))
			Expect(dst.Spec.Context).To(Equal("log"))
			Expect(dst.Spec.Filter).To(HaveLen(1))
			Expect(dst.Spec.Filter[0].Key).To(Equal("k8s.namespace.name"))
			Expect(dst.Spec.Filter[0].Operator).To(Equal("is"))
			Expect(*dst.Spec.Filter[0].Value).To(Equal("kube-system"))
		})

		It("should preserve data through a round-trip starting from v1alpha1", func() {
			original := &Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-spam-filter",
				},
				Spec: Dash0SpamFilterSpec{
					Contexts: []string{"log"},
					Filter: []Dash0SpamFilterCondition{
						{Key: "k8s.namespace.name", Operator: "is", Value: ptr.To("kube-system")},
						{Key: "level", Operator: "is_set"},
					},
				},
			}

			hub := &dash0v1alpha2.Dash0SpamFilter{}
			Expect(original.ConvertTo(hub)).To(Succeed())

			roundTripped := &Dash0SpamFilter{}
			Expect(roundTripped.ConvertFrom(hub)).To(Succeed())

			Expect(roundTripped.ObjectMeta).To(Equal(original.ObjectMeta))
			Expect(roundTripped.Spec.Contexts).To(Equal(original.Spec.Contexts))
			Expect(roundTripped.Spec.Filter).To(Equal(original.Spec.Filter))
		})
	})
})
