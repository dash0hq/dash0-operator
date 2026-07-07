// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestV1Beta1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "api/v1beta1 suite")
}
