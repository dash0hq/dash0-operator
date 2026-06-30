// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package exporters

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestExporters(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Exporters Suite")
}
