// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type stringifyContainerInstrumentationIssuesTest struct {
	instrumentationIssuesPerContainer map[string][]string
	expected                          string
}

var _ = Describe("workload events", func() {

	DescribeTable("stringify the instrumentation issue map", func(testConfig stringifyContainerInstrumentationIssuesTest) {
		Expect(stringifyContainerInstrumentationIssues(testConfig.instrumentationIssuesPerContainer)).To(Equal(testConfig.expected))
	}, Entry("nil map", stringifyContainerInstrumentationIssuesTest{
		instrumentationIssuesPerContainer: nil,
		expected:                          "",
	}), Entry("one container, one issue", stringifyContainerInstrumentationIssuesTest{
		instrumentationIssuesPerContainer: map[string][]string{
			"container-1": {"This did not work."},
		},
		expected: "container-1: This did not work.",
	}), Entry("one container, multiple issues", stringifyContainerInstrumentationIssuesTest{
		instrumentationIssuesPerContainer: map[string][]string{
			"container-1": {
				"This did not work.",
				"This also did not work.",
				"Something else failed.",
			},
		},
		expected: "container-1: This did not work. This also did not work. Something else failed.",
	}), Entry("multiple containers, one issue per container", stringifyContainerInstrumentationIssuesTest{
		instrumentationIssuesPerContainer: map[string][]string{
			"container-1": {"This did not work."},
			"container-2": {"This failed as well."},
		},
		expected: "container-1: This did not work. container-2: This failed as well.",
	}), Entry("multiple containers, multiple issues per container", stringifyContainerInstrumentationIssuesTest{
		instrumentationIssuesPerContainer: map[string][]string{
			"container-1": {
				"Failure 1.",
				"Failure 2.",
			},
			"container-2": {
				"Failure 3.",
				"Failure 4.",
			},
		},
		expected: "container-1: Failure 1. Failure 2. container-2: Failure 3. Failure 4.",
	}),
	)
})
