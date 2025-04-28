// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pointers", func() {
	It("ReadBoolPointerWithDefault", func() {
		Expect(ReadBoolPointerWithDefault(nil, false)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(nil, true)).To(BeTrue())
		Expect(ReadBoolPointerWithDefault(ptr.To(false), false)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(ptr.To(false), true)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(ptr.To(true), false)).To(BeTrue())
		Expect(ReadBoolPointerWithDefault(ptr.To(true), true)).To(BeTrue())
	})

	It("IsOptOutFlagWithDeprecatedVariantEnabled", func() {
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, nil)).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, ptr.To(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, ptr.To(true))).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(false), nil)).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(false), ptr.To(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(false), ptr.To(true))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(true), nil)).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(true), ptr.To(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(ptr.To(true), ptr.To(true))).To(BeTrue())
	})
})
