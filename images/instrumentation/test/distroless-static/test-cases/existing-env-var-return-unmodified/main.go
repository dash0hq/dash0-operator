package main

import (
	"fmt"
	"os"
)

func main() {
	name := "AN_ENVIRONMENT_VARIABLE"
	expected := "value"
	actual, exists := os.LookupEnv(name)
	if !exists {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"Unexpected value for the environment variable %s --\nexpected: %s\n     was: null\n",
			name,
			expected,
		)
		os.Exit(1)
	}
	if actual != "value" {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"Unexpected value for the environment variable %s --\nexpected: %s\n     was: %s\n",
			name,
			expected,
			actual,
		)
		os.Exit(1)
	}
	os.Exit(0)
}
