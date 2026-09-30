// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"os"
	"regexp"
	"testing"
)

const (
	dockerfilePath = "../../Dockerfile"
	goModPath      = "../go.mod"
)

// kubectlImagePattern matches the stage of the Dockerfile that provides the kubectl binary, e.g.
// "FROM registry.k8s.io/kubectl:v1.37.1 AS kubectl", and captures the minor and patch version.
var kubectlImagePattern = regexp.MustCompile(`(?m)^FROM\s+registry\.k8s\.io/kubectl:v1\.(\d+)\.(\d+)\s`)

// kubernetesGoModulesUsedForParsing are the Go modules parseKubectlArguments relies on to resolve an argument list the
// same way the kubectl binary does. They are released in lockstep with kubectl, v0.x.y of each module belongs to
// kubectl v1.x.y.
var kubernetesGoModulesUsedForParsing = []string{
	"k8s.io/kubectl",
	"k8s.io/cli-runtime",
	"k8s.io/component-base",
}

// TestKubectlBinaryAndGoModuleVersionsMatch makes sure the kubectl binary that executes command requests (see
// images/agent0-connector/Dockerfile) and the kubectl Go modules that parseKubectlArguments uses to resolve their
// argument lists have the same version. If they differ, kubectl might execute a different command than the one the
// connector has validated.
func TestKubectlBinaryAndGoModuleVersionsMatch(t *testing.T) {
	dockerfile, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dockerfilePath, err)
	}
	imageVersion := kubectlImagePattern.FindSubmatch(dockerfile)
	if imageVersion == nil {
		t.Fatalf("cannot find the kubectl image (registry.k8s.io/kubectl:v1.x.y) in %s", dockerfilePath)
	}
	expectedModuleVersion := "v0." + string(imageVersion[1]) + "." + string(imageVersion[2])

	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", goModPath, err)
	}
	for _, module := range kubernetesGoModulesUsedForParsing {
		modulePattern := regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(module) + `\s+(v\S+)`)
		moduleVersion := modulePattern.FindSubmatch(goMod)
		if moduleVersion == nil {
			t.Errorf("cannot find the Go module %s in %s", module, goModPath)
			continue
		}
		if string(moduleVersion[1]) != expectedModuleVersion {
			t.Errorf(
				"the kubectl binary in %s has version v1.%s.%s, which requires the Go module %s in version %s, but %s "+
					"requires %s; update both to the same kubectl release",
				dockerfilePath,
				imageVersion[1],
				imageVersion[2],
				module,
				expectedModuleVersion,
				goModPath,
				moduleVersion[1],
			)
		}
	}
}
