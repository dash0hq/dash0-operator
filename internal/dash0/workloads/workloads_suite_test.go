// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorkloadModifications(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workload Modifications Suite")
}
