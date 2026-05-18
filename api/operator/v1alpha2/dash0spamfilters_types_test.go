// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha2_test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1alpha2 "github.com/dash0hq/dash0-operator/api/operator/v1alpha2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("v1alpha2 Dash0 spam filter CRD", func() {

	It("should be marked as the conversion hub", func() {
		var _ conversion.Hub = &dash0v1alpha2.Dash0SpamFilter{}
		(&dash0v1alpha2.Dash0SpamFilter{}).Hub()
	})

	Describe("converting to and from the v1alpha1 spoke", func() {

		It("should convert v1alpha2 to v1alpha1 (context → contexts)", func() {
			src := &dash0v1alpha2.Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-spam-filter",
				},
				Spec: dash0v1alpha2.Dash0SpamFilterSpec{
					Context: "span",
					Filter: []dash0v1alpha2.Dash0SpamFilterCondition{
						{
							Key:      "service.name",
							Operator: "is_not",
							Value:    ptr.To("noisy-service"),
						},
					},
				},
			}

			dst := &dash0v1alpha1.Dash0SpamFilter{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			Expect(dst.ObjectMeta).To(Equal(src.ObjectMeta))
			Expect(dst.Spec.Contexts).To(Equal([]string{"span"}))
			Expect(dst.Spec.Filter).To(HaveLen(1))
			Expect(dst.Spec.Filter[0].Key).To(Equal("service.name"))
			Expect(dst.Spec.Filter[0].Operator).To(Equal("is_not"))
			Expect(*dst.Spec.Filter[0].Value).To(Equal("noisy-service"))
		})

		It("should preserve data through a round-trip starting from v1alpha2", func() {
			original := &dash0v1alpha2.Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-spam-filter",
				},
				Spec: dash0v1alpha2.Dash0SpamFilterSpec{
					Context: "log",
					Filter: []dash0v1alpha2.Dash0SpamFilterCondition{
						{Key: "k8s.namespace.name", Operator: "is", Value: ptr.To("kube-system")},
						{Key: "level", Operator: "is_set"},
					},
				},
			}

			spoke := &dash0v1alpha1.Dash0SpamFilter{}
			Expect(spoke.ConvertFrom(original)).To(Succeed())

			roundTripped := &dash0v1alpha2.Dash0SpamFilter{}
			Expect(spoke.ConvertTo(roundTripped)).To(Succeed())

			Expect(roundTripped.ObjectMeta).To(Equal(original.ObjectMeta))
			Expect(roundTripped.Spec.Context).To(Equal(original.Spec.Context))
			Expect(roundTripped.Spec.Filter).To(Equal(original.Spec.Filter))
		})

		It("should handle empty context when round-tripping (no Contexts entry produced)", func() {
			original := &dash0v1alpha2.Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-spam-filter",
				},
				Spec: dash0v1alpha2.Dash0SpamFilterSpec{
					Context: "",
					Filter: []dash0v1alpha2.Dash0SpamFilterCondition{
						{Key: "level", Operator: "is_set"},
					},
				},
			}

			spoke := &dash0v1alpha1.Dash0SpamFilter{}
			Expect(spoke.ConvertFrom(original)).To(Succeed())
			Expect(spoke.Spec.Contexts).To(BeEmpty())

			roundTripped := &dash0v1alpha2.Dash0SpamFilter{}
			Expect(spoke.ConvertTo(roundTripped)).To(Succeed())
			Expect(roundTripped.Spec.Context).To(BeEmpty())
		})
	})
})
