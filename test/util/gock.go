// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	"github.com/h2non/gock"
	. "github.com/onsi/gomega"
)

// VerifyNoUnmatchedGockRequests makes sure that gock did not silently swallow any unmatched requests during a test.
// Such requests indicate missing or wrong mocks and previously caused tests to be flaky, when leftover processing
// and/or HTTP retries wrote bogus status while the next test had already started.
func VerifyNoUnmatchedGockRequests() {
	unmatched := gock.GetUnmatchedRequests()
	unmatchedDescriptions := make([]string, 0, len(unmatched))
	for _, req := range unmatched {
		unmatchedDescriptions = append(
			unmatchedDescriptions,
			fmt.Sprintf("%s %s", req.Method, req.URL.String()),
		)
	}
	gock.CleanUnmatchedRequest()
	Expect(unmatchedDescriptions).To(
		BeEmpty(),
		"gock recorded HTTP requests that did not match any mock",
	)
}
