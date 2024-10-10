// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

func e2ePrint(format string, a ...any) {
	fmt.Fprintf(GinkgoWriter, format, a...)
}
