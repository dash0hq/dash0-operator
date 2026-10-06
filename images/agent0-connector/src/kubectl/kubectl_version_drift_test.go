// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
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

// cliParsingGoModules are the Go modules that implement kubectl's command line parsing. They are not released in
// lockstep with kubectl. The kubectl binary is built from the k8s.io/kubernetes Go workspace, and the versions selected
// there are the ones that k8s.io/kubectl requires: hack/update-vendor.sh in kubernetes/kubernetes runs "go work sync",
// which writes the versions selected for the workspace back into the go.mod file of every staging module, including
// k8s.io/kubectl, from which the published module is created.
var cliParsingGoModules = []string{
	"github.com/spf13/cobra",
	"github.com/spf13/pflag",
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

// TestCliParsingGoModuleVersionsMatchKubectl makes sure parseKubectlArguments uses the same cobra and pflag versions
// that k8s.io/kubectl requires, which are the versions the kubectl binary has been built with. If they differ, kubectl
// might parse an argument list differently than the connector has validated it.
func TestCliParsingGoModuleVersionsMatchKubectl(t *testing.T) {
	kubectlGoModPath, err := exec.Command("go", "list", "-m", "-f", "{{.GoMod}}", "k8s.io/kubectl").Output()
	if err != nil {
		t.Fatalf("cannot determine the go.mod file of k8s.io/kubectl: %v", err)
	}
	kubectlGoMod, err := os.ReadFile(strings.TrimSpace(string(kubectlGoModPath)))
	if err != nil {
		t.Fatalf("cannot read the go.mod file of k8s.io/kubectl: %v", err)
	}
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", goModPath, err)
	}
	for _, module := range cliParsingGoModules {
		modulePattern := regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(module) + `\s+(v\S+)`)
		requiredByKubectl := modulePattern.FindSubmatch(kubectlGoMod)
		if requiredByKubectl == nil {
			t.Errorf("cannot find the Go module %s in the go.mod file of k8s.io/kubectl", module)
			continue
		}
		moduleVersion := modulePattern.FindSubmatch(goMod)
		if moduleVersion == nil {
			t.Errorf("cannot find the Go module %s in %s", module, goModPath)
			continue
		}
		if string(moduleVersion[1]) != string(requiredByKubectl[1]) {
			t.Errorf(
				"k8s.io/kubectl requires the Go module %s in version %s, but %s requires %s; the kubectl binary of "+
					"the same release is built with %s, update %s to the same version",
				module,
				requiredByKubectl[1],
				goModPath,
				moduleVersion[1],
				requiredByKubectl[1],
				module,
			)
		}
	}
}
