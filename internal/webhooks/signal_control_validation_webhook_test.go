// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validateOperationProcessorCardinalityRules", func() {

	It("should accept an empty rule list", func() {
		Expect(validateOperationProcessorCardinalityRules(nil)).To(Succeed())
	})

	It("should accept a rule whose regex compiles and whose capture groups match the replacements", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "warehouse-sites",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z0-9-]+)/", Replacements: []string{"site"}},
					{Regex: "/zones/([a-z]+)/([0-9]+)/", Replacements: []string{"zone", "id"}},
				},
			},
		}
		Expect(validateOperationProcessorCardinalityRules(rules)).To(Succeed())
	})

	It("should reject a rule whose regex does not compile", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "broken-regex",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z", Replacements: []string{"site"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operationProcessor.cardinalityRules[0] (broken-regex) operationMatchers[0]"))
		Expect(err.Error()).To(ContainSubstring("invalid regex"))
	})

	It("should reject a matcher whose capture group count differs from the replacement count", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id: "count-mismatch",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/sites/([a-z]+)/([0-9]+)/", Replacements: []string{"site"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("regex has 2 capture group(s) but 1 replacement(s) provided"))
	})

	It("should report the offending rule and matcher indices across multiple rules", func() {
		rules := []dash0v1alpha1.CardinalityRule{
			{
				Id:                "ok",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{{Regex: "/a/([0-9]+)", Replacements: []string{"n"}}},
			},
			{
				Id: "bad",
				OperationMatchers: []dash0v1alpha1.OperationMatcher{
					{Regex: "/a/([0-9]+)", Replacements: []string{"n"}},
					{Regex: "/b/([0-9]+)", Replacements: []string{"n", "extra"}},
				},
			},
		}
		err := validateOperationProcessorCardinalityRules(rules)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operationProcessor.cardinalityRules[1] (bad) operationMatchers[1]"))
	})
})
