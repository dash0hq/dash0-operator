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

		It("should truncate long image names and leave the repo and tag unchanged if those parts are < 63 chars", func() {
			labelFromLongImageName := ImageRefToLabel(
				"some.very.long.registry.that.needs.to.be.truncated.io/dash0hq/operator-controller@latest",
			)
			Expect(labelFromLongImageName).To(
				Equal(
					"operator-controller_latest",
				),
			)
			Expect(len(labelFromLongImageName)).To(BeNumerically("<", 63))
		})
	})
})

var _ = Describe("MergeMaps", func() {
	It("should return nil when both maps are empty", func() {
		Expect(MergeMaps(nil, nil)).To(BeNil())
		Expect(MergeMaps(map[string]string{}, map[string]string{})).To(BeNil())
	})

	It("should return the defaults map when there are no custom entries", func() {
		defaults := map[string]string{"default-key": "default-value"}
		Expect(MergeMaps(defaults, nil)).To(Equal(defaults))
		Expect(MergeMaps(defaults, map[string]string{})).To(Equal(defaults))
	})

	It("should return the custom map when there are no default entries", func() {
		custom := map[string]string{"custom-key": "custom-value"}
		Expect(MergeMaps(nil, custom)).To(Equal(custom))
		Expect(MergeMaps(map[string]string{}, custom)).To(Equal(custom))
	})

	It("should merge default and custom entries when there is no overlap", func() {
		defaults := map[string]string{"default-key": "default-value"}
		custom := map[string]string{"custom-key": "custom-value"}
		Expect(MergeMaps(defaults, custom)).To(Equal(map[string]string{
			"default-key": "default-value",
			"custom-key":  "custom-value",
		}))
	})

	It("should let default entries take precedence over custom entries on conflicting keys", func() {
		defaults := map[string]string{"shared-key": "default-value", "default-key": "default-value"}
		custom := map[string]string{"shared-key": "custom-value", "custom-key": "custom-value"}
		Expect(MergeMaps(defaults, custom)).To(Equal(map[string]string{
			"shared-key":  "default-value",
			"default-key": "default-value",
			"custom-key":  "custom-value",
		}))
	})

	It("should not mutate the input maps", func() {
		defaults := map[string]string{"default-key": "default-value"}
		custom := map[string]string{"custom-key": "custom-value"}
		MergeMaps(defaults, custom)
		Expect(defaults).To(Equal(map[string]string{"default-key": "default-value"}))
		Expect(custom).To(Equal(map[string]string{"custom-key": "custom-value"}))
	})

	It("should always return a copy that can be mutated without affecting the inputs", func() {
		defaults := map[string]string{"default-key": "default-value"}
		mergedDefaultsOnly := MergeMaps(defaults, nil)
		mergedDefaultsOnly["injected-key"] = "injected-value"
		Expect(defaults).To(Equal(map[string]string{"default-key": "default-value"}))

		custom := map[string]string{"custom-key": "custom-value"}
		mergedCustomOnly := MergeMaps(nil, custom)
		mergedCustomOnly["injected-key"] = "injected-value"
		Expect(custom).To(Equal(map[string]string{"custom-key": "custom-value"}))
	})
})
