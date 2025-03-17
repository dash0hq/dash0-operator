// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MRU", func() {

	It("MRU Len/IsEmpty", func() {
		mru := NewMru[int](3)
		Expect(mru.IsEmpty()).To(BeTrue())
		Expect(mru.Len()).To(Equal(0))
		mru.Put(1)
		Expect(mru.IsEmpty()).To(BeFalse())
		Expect(mru.Len()).To(Equal(1))
		mru.Put(1)
		Expect(mru.IsEmpty()).To(BeFalse())
		Expect(mru.Len()).To(Equal(2))
	})

	It("Put and Take keeps order", func() {
		mru := NewMru[int](10)
		Expect(mru.IsEmpty()).To(BeTrue())
		mru.Put(1)
		Expect(mru.IsEmpty()).To(BeFalse())
		mru.Put(2)
		mru.Put(3)
		Expect(*mru.Take()).To(Equal(1))
		Expect(*mru.Take()).To(Equal(2))
		Expect(mru.IsEmpty()).To(BeFalse())
		Expect(*mru.Take()).To(Equal(3))
		Expect(mru.IsEmpty()).To(BeTrue())
		Expect(mru.Take()).To(BeNil())
	})

	It("Limit", func() {
		mru := NewMru[int](3)
		Expect(mru.IsEmpty()).To(BeTrue())
		mru.Put(1)
		mru.Put(2)
		mru.Put(3)
		Expect(mru.Len()).To(Equal(3))
		Expect(mru.elements[0]).To(Equal(1))
		Expect(mru.elements[1]).To(Equal(2))
		Expect(mru.elements[2]).To(Equal(3))
		mru.Put(4)
		Expect(mru.Len()).To(Equal(3))
		Expect(mru.elements[0]).To(Equal(2))
		Expect(mru.elements[1]).To(Equal(3))
		Expect(mru.elements[2]).To(Equal(4))
		mru.Put(5)
		Expect(mru.Len()).To(Equal(3))
		Expect(mru.elements[0]).To(Equal(3))
		Expect(mru.elements[1]).To(Equal(4))
		Expect(mru.elements[2]).To(Equal(5))
	})

	It("ForAllAndClean", func() {
		mru := NewMru[int](3)
		mru.Put(1)
		mru.Put(2)
		mru.Put(3)
		var sum int
		mru.ForAllAndClean(func(element int) {
			sum += element
		})
		Expect(sum).To(Equal(6))
		Expect(mru.IsEmpty()).To(BeTrue())
	})
})
