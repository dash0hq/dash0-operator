// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pointer utils", func() {
	It("ReadBoolPointerWithDefault", func() {
		Expect(ReadBoolPointerWithDefault(nil, false)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(nil, true)).To(BeTrue())
		Expect(ReadBoolPointerWithDefault(new(false), false)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(new(false), true)).To(BeFalse())
		Expect(ReadBoolPointerWithDefault(new(true), false)).To(BeTrue())
		Expect(ReadBoolPointerWithDefault(new(true), true)).To(BeTrue())
	})

	It("IsOptOutFlagWithDeprecatedVariantEnabled", func() {
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, nil)).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, new(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(nil, new(true))).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(false), nil)).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(false), new(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(false), new(true))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(true), nil)).To(BeTrue())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(true), new(false))).To(BeFalse())
		Expect(IsOptOutFlagWithDeprecatedVariantEnabled(new(true), new(true))).To(BeTrue())
	})

	It("IsEmpty", func() {
		Expect(IsEmpty(nil)).To(BeTrue())
		Expect(IsEmpty(new(""))).To(BeTrue())
		Expect(IsEmpty(new("  "))).To(BeTrue())
		Expect(IsEmpty(new(" \t \n "))).To(BeTrue())
		Expect(IsEmpty(new("..."))).To(BeFalse())
	})

	It("IsStringPointerValueDifferent", func() {
		Expect(IsStringPointerValueDifferent(nil, nil)).To(BeFalse())
		Expect(IsStringPointerValueDifferent(nil, new(""))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(nil, new("  "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(nil, new(" \t \n "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(nil, new("..."))).To(BeTrue())

		Expect(IsStringPointerValueDifferent(new(""), nil)).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new(""), new(""))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new(""), new("  "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new(""), new(" \t \n "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new(""), new("..."))).To(BeTrue())

		Expect(IsStringPointerValueDifferent(new("  "), nil)).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  "), new(""))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  "), new("  "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  "), new(" \t \n "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  "), new("..."))).To(BeTrue())

		Expect(IsStringPointerValueDifferent(new("  \t \n "), nil)).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  \t \n "), new(""))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  \t \n "), new("  "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  \t \n "), new(" \t \n "))).To(BeFalse())
		Expect(IsStringPointerValueDifferent(new("  \t \n "), new("..."))).To(BeTrue())

		Expect(IsStringPointerValueDifferent(new("..."), nil)).To(BeTrue())
		Expect(IsStringPointerValueDifferent(new("..."), new(""))).To(BeTrue())
		Expect(IsStringPointerValueDifferent(new("..."), new("  "))).To(BeTrue())
		Expect(IsStringPointerValueDifferent(new("..."), new(" \t \n "))).To(BeTrue())
		Expect(IsStringPointerValueDifferent(new("..."), new("..."))).To(BeFalse())
	})
})
