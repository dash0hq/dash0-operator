// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "api/operator/common suite")
}
