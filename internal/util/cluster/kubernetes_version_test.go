// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"k8s.io/apimachinery/pkg/version"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster utilities", func() {

	Describe("parseKubernetesVersion", func() {
		type parseVersionTest struct {
			major            string
			minor            string
			expectedMajor    int
			expectedMinor    int
			expectedString   string
			expectedDetected bool
		}

		DescribeTable("should parse the major/minor of a Kubernetes version",
			func(testConfig parseVersionTest) {
				info, detected := parseKubernetesVersion(
					&version.Info{Major: testConfig.major, Minor: testConfig.minor},
					logd.Discard(),
				)
				Expect(detected).To(Equal(testConfig.expectedDetected))
				Expect(info.VersionString).To(Equal(testConfig.expectedString))
				if !testConfig.expectedDetected {
					Expect(info.Major).To(Equal(0))
					Expect(info.Minor).To(Equal(0))
					return
				}
				Expect(info.Major).To(Equal(testConfig.expectedMajor))
				Expect(info.Minor).To(Equal(testConfig.expectedMinor))
			},
			Entry("1.36", parseVersionTest{
				major:            "1",
				minor:            "36",
				expectedMajor:    1,
				expectedMinor:    36,
				expectedString:   "1.36",
				expectedDetected: true,
			}),
			Entry("1.31", parseVersionTest{
				major:            "1",
				minor:            "31",
				expectedMajor:    1,
				expectedMinor:    31,
				expectedString:   "1.31",
				expectedDetected: true,
			}),
			Entry("GKE-style minor with plus suffix", parseVersionTest{
				major:            "1",
				minor:            "31+",
				expectedMajor:    1,
				expectedMinor:    31,
				expectedString:   "1.31+",
				expectedDetected: true,
			}),
			Entry("EKS-style minor with text suffix", parseVersionTest{
				major:            "1",
				minor:            "30-eks-abc1234",
				expectedMajor:    1,
				expectedMinor:    30,
				expectedString:   "1.30-eks-abc1234",
				expectedDetected: true,
			}),
			Entry("hypothetical major version 2", parseVersionTest{
				major:            "2",
				minor:            "0",
				expectedMajor:    2,
				expectedMinor:    0,
				expectedString:   "2.0",
				expectedDetected: true,
			}),
			Entry("2.34", parseVersionTest{
				major:            "2",
				minor:            "34",
				expectedMajor:    2,
				expectedMinor:    34,
				expectedString:   "2.34",
				expectedDetected: true,
			}),
			Entry("empty major", parseVersionTest{
				major:            "",
				minor:            "31",
				expectedString:   ".31",
				expectedDetected: false,
			}),
			Entry("empty minor", parseVersionTest{
				major:            "1",
				minor:            "",
				expectedString:   "1.",
				expectedDetected: false,
			}),
			Entry("both empty", parseVersionTest{
				major:            "",
				minor:            "",
				expectedString:   ".",
				expectedDetected: false,
			}),
			Entry("non-numeric major", parseVersionTest{
				major:            "abc",
				minor:            "31",
				expectedString:   "abc.31",
				expectedDetected: false,
			}),
			Entry("non-numeric minor", parseVersionTest{
				major:            "1",
				minor:            "thirty-one",
				expectedString:   "1.thirty-one",
				expectedDetected: false,
			}),
			Entry("minor with leading non-digit", parseVersionTest{
				major:            "1",
				minor:            "+31",
				expectedString:   "1.+31",
				expectedDetected: false,
			}),
		)
	})

	Describe("extractLeadingDigits", func() {
		type extractLeadingDigitsTest struct {
			input       string
			expected    int
			expectError bool
		}

		DescribeTable("should extract the leading digits of a string",
			func(testConfig extractLeadingDigitsTest) {
				result, err := extractLeadingDigits(testConfig.input)
				if testConfig.expectError {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(testConfig.expected))
			},
			Entry("single digit", extractLeadingDigitsTest{
				input:    "1",
				expected: 1,
			}),
			Entry("multiple digits", extractLeadingDigitsTest{
				input:    "36",
				expected: 36,
			}),
			Entry("zero", extractLeadingDigitsTest{
				input:    "0",
				expected: 0,
			}),
			Entry("GKE-style suffix (digits followed by '+')", extractLeadingDigitsTest{
				input:    "31+",
				expected: 31,
			}),
			Entry("minor version with patch suffix", extractLeadingDigitsTest{
				input:    "31.0",
				expected: 31,
			}),
			Entry("EKS-style suffix (digits followed by '-text')", extractLeadingDigitsTest{
				input:    "31-eks-abc1234",
				expected: 31,
			}),
			Entry("digits followed by trailing whitespace", extractLeadingDigitsTest{
				input:    "31 ",
				expected: 31,
			}),
			Entry("empty string", extractLeadingDigitsTest{
				input:       "",
				expectError: true,
			}),
			Entry("no leading digits, only letters", extractLeadingDigitsTest{
				input:       "abc",
				expectError: true,
			}),
			Entry("leading non-digit (plus)", extractLeadingDigitsTest{
				input:       "+31",
				expectError: true,
			}),
			Entry("leading whitespace", extractLeadingDigitsTest{
				input:       " 31",
				expectError: true,
			}),
			Entry("leading dot", extractLeadingDigitsTest{
				input:       ".31",
				expectError: true,
			}),
		)
	})
})
