// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/validation"
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

		It("should leave image refs with exactly 63 characters unchanged apart from character conversion", func() {
			imageRef := "registry.example.com/dash0hq/operator-controller:0.155.1-abcdef"
			Expect(imageRef).To(HaveLen(63))
			Expect(ImageRefToLabel(imageRef)).To(Equal("registry.example.com_dash0hq_operator-controller_0.155.1-abcdef"))
		})

		It("should replace the registry and organization of long image refs with a hash", func() {
			labelFromLongImageName := ImageRefToLabel(
				"europe-docker.pkg.dev/dash0-deployment/dash0-europe-remote-ghcr-io/dash0hq/instrumentation:0.155.1",
			)
			Expect(labelFromLongImageName).To(Equal("instrumentation_0.155.1_b064cbcb42e788ec"))
			expectValidLabelValue(labelFromLongImageName)
		})

		It("should truncate the readable part of long image refs with a digest", func() {
			labelFromLongImageName := ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bcd3c6b",
			)
			Expect(labelFromLongImageName).To(Equal("operator-controller_sha256_68d83fa12931ba8c085_e28edaf62d1d30fd"))
			Expect(labelFromLongImageName).To(HaveLen(63))
			expectValidLabelValue(labelFromLongImageName)
		})

		It("should not end the readable part of long image refs with an invalid character", func() {
			labelFromLongImageName := ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c08-5d8804f5a60d0b3df68494903a7a5b6506928e0bc",
			)
			Expect(labelFromLongImageName).To(Equal("operator-controller_sha256_68d83fa12931ba8c08_c0ec845b478e8ee0"))
			expectValidLabelValue(labelFromLongImageName)
		})

		It("should distinguish long image refs that only differ in the registry", func() {
			Expect(ImageRefToLabel(
				"some.very.long.registry.that.needs.to.be.truncated.io/dash0hq/instrumentation:0.155.1",
			)).NotTo(Equal(ImageRefToLabel(
				"another.very.long.registry.that.needs.to.be.truncated.io/dash0hq/instrumentation:0.155.1",
			)))
		})

		It("should distinguish long image refs that only differ in the organization", func() {
			Expect(ImageRefToLabel(
				"some.very.long.registry.that.needs.to.be.truncated.io/dash0hq/instrumentation:0.155.1",
			)).NotTo(Equal(ImageRefToLabel(
				"some.very.long.registry.that.needs.to.be.truncated.io/otherorg/instrumentation:0.155.1",
			)))
		})

		It("should distinguish long image refs that only differ after the truncated part of the last path segment", func() {
			Expect(ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bcd3c6b",
			)).NotTo(Equal(ImageRefToLabel(
				"ghcr.io/dash0hq/operator-controller@sha256:68d83fa12931ba8c085d8804f5a60d0b3df68494903a7a5b6506928e0bcd3c6c",
			)))
		})

		It("should only use the hash if the last path segment of a long image ref has no valid characters", func() {
			labelFromLongImageName := ImageRefToLabel(
				"some.very.long.registry.that.needs.to.be.truncated.io/dash0hq/instrumentation/___",
			)
			Expect(labelFromLongImageName).To(Equal("1fa12217110fa8af"))
			expectValidLabelValue(labelFromLongImageName)
		})
	})
})

func expectValidLabelValue(value string) {
	GinkgoHelper()
	Expect(validation.IsValidLabelValue(value)).To(BeEmpty())
}

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
