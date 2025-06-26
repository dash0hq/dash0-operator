// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
)

func main() {
	envVarName := "STATICALLY_BUILT_TEST_VAR"
	value, isSet := os.LookupEnv(envVarName)
	if !isSet {
		fmt.Printf("The environmet variable \"%s\" is not set.\n", envVarName)
		os.Exit(0)
	}
	fmt.Printf("The environmet variable \"%s\" had the value: \"%s\".\n", envVarName, value)
	os.Exit(0)
}
