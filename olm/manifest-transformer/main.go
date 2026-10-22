// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Command manifest-transformer turns the output of "helm template" for the Dash0 operator Helm chart into the manifests
// that are handed to "operator-sdk generate bundle" when the OLM bundle is generated. It reads the rendered chart from
// stdin, writes the transformed manifests to stdout and reports every change on stderr.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	version := flag.String("version", "", "The bundle version, used for the app.kubernetes.io/version labels.")
	flag.Parse()
	if *version == "" {
		fmt.Fprintf(os.Stderr, "error: --version is required\n")
		os.Exit(2)
	}
	if err := transform(os.Stdin, os.Stdout, os.Stderr, *version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
