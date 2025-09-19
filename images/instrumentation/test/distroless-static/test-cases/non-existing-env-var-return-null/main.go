package main

import (
	"fmt"
	"os"
)

func main() {
	name := "DOES_NOT_EXIST"
	actual, exists := os.LookupEnv(name)
	if exists {
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
