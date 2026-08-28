// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// agent0ConnectorDummyCrdManifest defines a resource type the operator's default cluster role for the
	// agent0-connector does not cover, which is what the e2e test for a custom cluster role needs.
	agent0ConnectorDummyCrdManifest      = "test/e2e/agent0connectore2edummy-crd.yaml"
	agent0ConnectorDummyResourceManifest = "test/e2e/agent0connectore2edummy-resource.yaml"

	agent0ConnectorDummyCrdName      = "agent0connectore2edummies.e2e.dash0.com"
	agent0ConnectorDummyApiGroup     = "e2e.dash0.com"
	agent0ConnectorDummyResourceType = "agent0connectore2edummies"
	agent0ConnectorDummyResourceName = "agent0-connector-e2e-dummy"
)

// deployAgent0ConnectorDummyResource deploys the dummy custom resource definition together with one instance of it. It
// waits for the CRD to become established, otherwise creating the instance would race with the API server registering
// the new resource type.
func deployAgent0ConnectorDummyResource() {
	By("deploying the dummy CRD for the agent0-connector custom cluster role test")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		agent0ConnectorDummyCrdManifest,
	))).To(Succeed())

	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--for=condition=Established",
		"crd/"+agent0ConnectorDummyCrdName,
		"--timeout=30s",
	))).To(Succeed())

	By("deploying an instance of the dummy resource type")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		agent0ConnectorDummyResourceManifest,
	))).To(Succeed())
}

// removeAgent0ConnectorDummyResource deletes the dummy custom resource definition, which deletes its instances as well.
func removeAgent0ConnectorDummyResource() {
	By("removing the dummy CRD for the agent0-connector custom cluster role test")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		agent0ConnectorDummyCrdManifest,
	))).To(Succeed())
}
