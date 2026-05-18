// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha2_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestV1Alpha2(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "api/v1alpha2 suite")
}
