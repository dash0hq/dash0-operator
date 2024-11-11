// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	//nolint:golint,revive
	. "github.com/onsi/gomega"
)

func deployOtlpSink(workingDir string) {
	originalManifest := fmt.Sprintf(
		"%s/test-resources/otlp-sink/otlp-sink.yaml",
		workingDir,
	)
	var e2eTestExportDir string
	if !isKindCluster() {
		e2eTestExportDir = fmt.Sprintf(
			"%s/test-resources/e2e-test-volumes/otlp-sink",
			workingDir,
		)
	}

	tmpFile, err := os.CreateTemp(os.TempDir(), "otlp-sink-*.yaml")
	if err != nil {
		log.Fatalf("could not create temporary file to store the patched otlp-sink manifest: %v", err)
	}
	defer func() {
		Expect(os.Remove(tmpFile.Name())).To(Succeed())
	}()

	Expect(func() error {
		manifest, err := os.ReadFile(originalManifest)
		if err != nil {
			return fmt.Errorf("could not read otlp-sink manifest: %w", err)
		}

		if e2eTestExportDir != "" {
			manifest = []byte(strings.ReplaceAll(string(manifest), "path: /tmp/telemetry", "path: "+e2eTestExportDir))
		}
		if err = os.WriteFile(tmpFile.Name(), manifest, 0644); err != nil {
			return fmt.Errorf("could not write patched manifest to temporary file: %w", err)
		}

		return nil
	}()).To(Succeed())

	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"apply",
				"-f",
				tmpFile.Name(),
			))).To(Succeed())

	Expect(
		runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"deployment",
				"otlp-sink",
				"--namespace",
				"otlp-sink",
				"--timeout",
				"1m",
			),
		),
	).To(Succeed())
}

func uninstallOtlpSink(workingDir string) {
	originalManifest := fmt.Sprintf(
		"%s/test-resources/otlp-sink/otlp-sink.yaml",
		workingDir,
	)

	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"delete",
				"--ignore-not-found=true",
				"-f",
				originalManifest,
				"--wait",
			))).To(Succeed())

}
