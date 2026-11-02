// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCrds(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "OLM CRDs Suite")
}
