// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dash0 Utils", func() {

	Describe("converting labels", func() {
		It("should leave normal characters untouched", func() {
			Expect(ImageNameToLabel("instrumentation")).To(Equal("instrumentation"))
		})

		It("should convert : to _", func() {
			Expect(ImageNameToLabel("instrumentation:latest")).To(Equal("instrumentation_latest"))
		})

		It("should convert / to _", func() {
			Expect(ImageNameToLabel("ghcr.io/dash0hq/operator-controller:0.3.0")).To(Equal("ghcr.io_dash0hq_operator-controller_0.3.0"))
		})

		It("should convert @ to _", func() {
			Expect(
				ImageNameToLabel(
					"ghcr.io/dash0hq/operator-controller@sha256:123",
				),
			).To(
				Equal(
					"ghcr.io_dash0hq_operator-controller_sha256_123",
				),
			)
		})

		It("should truncate long image names", func() {
			labelFromLongImageName := ImageNameToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bcd3c6b",
			)
			Expect(labelFromLongImageName).To(
				Equal(
					"ghcr.io_dash0hq_operator-controller_sha256_68d83fa12931ba8c085d",
				),
			)
			Expect(labelFromLongImageName).To(HaveLen(63))
		})
	})
})
