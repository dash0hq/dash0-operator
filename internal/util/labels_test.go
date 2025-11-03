// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("labels", func() {

	Describe("converting labels", func() {
		It("should leave normal characters untouched", func() {
			Expect(ImageRefToLabel("instrumentation")).To(Equal("instrumentation"))
		})

		It("should convert : to _", func() {
			Expect(ImageRefToLabel("instrumentation:latest")).To(Equal("instrumentation_latest"))
		})

		It("should convert / to _", func() {
			Expect(ImageRefToLabel("ghcr.io/dash0hq/operator-controller:0.3.0")).To(Equal("ghcr.io_dash0hq_operator-controller_0.3.0"))
		})

		It("should convert @ to _", func() {
			Expect(
				ImageRefToLabel(
					"ghcr.io/dash0hq/operator-controller@sha256:123",
				),
			).To(
				Equal(
					"ghcr.io_dash0hq_operator-controller_sha256_123",
				),
			)
		})

		It("should truncate long image names", func() {
			labelFromLongImageName := ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bcd3c6b",
			)
			Expect(labelFromLongImageName).To(
				Equal(
					"operator-controller_sha256_68d83fa12931ba8c085d8804f5a60d0b3df6",
				),
			)
			Expect(labelFromLongImageName).To(HaveLen(63))
		})

		It("should truncate long image names when registry part contains port", func() {
			labelFromLongImageName := ImageRefToLabel(
				"ghcr.io:8080/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bc",
			)
			Expect(labelFromLongImageName).To(
				Equal(
					"operator-controller_sha256_68d83fa12931ba8c085d8804f5a60d0b3df6",
				),
			)
			Expect(labelFromLongImageName).To(HaveLen(63))
		})

		It("should truncate long image names and ensure the label value does not end with an invalid character", func() {
			labelFromLongImageName := ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df-68494903a7a5b6506928e0bcd3c6b",
			)
			Expect(labelFromLongImageName).To(
				Equal(
					"operator-controller_sha256_68d83fa12931ba8c085d8804f5a60d0b3df",
				),
			)
			Expect(labelFromLongImageName).To(HaveLen(62))
		})
	})
})
