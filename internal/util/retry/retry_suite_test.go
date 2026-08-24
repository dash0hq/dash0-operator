// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// The tests for this package live in the external test package retry_test, since they use the shared test helpers from
// test/util, which in turn depend on this package.
package retry_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRetry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Retry Util Suite")
}
