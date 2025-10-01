package main

import (
	"fmt"
	"os"
)

func main() {
	name := "OTEL_RESOURCE_ATTRIBUTES"
	actual, exists := os.LookupEnv(name)
	if exists {
		// For a statically compiled app without any libc bindings, we expect that we cannot inject
		// OTEL_RESOURCE_ATTRIBUTES, so this should be unset.
		_, _ = fmt.Fprintf(
			os.Stderr,
			"Unexpected value for the environment variable %s -- expected: null, was: %s\n",
			name,
			actual,
		)
		os.Exit(1)
	}
	os.Exit(0)
}
