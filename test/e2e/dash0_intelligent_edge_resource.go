// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const intelligentEdgeName = "dash0-intelligent-edge"

type intelligentEdgeValues struct {
	DecisionMakerEndpoint   string
	ControlPlaneApiEndpoint string
}

var (
	//go:embed dash0intelligentedge.e2e.yaml.template
	intelligentEdgeSource   string
	intelligentEdgeTemplate *template.Template
)

func renderIntelligentEdgeTemplate(values intelligentEdgeValues) string {
	intelligentEdgeTemplate = initTemplateOnce(
		intelligentEdgeTemplate,
		intelligentEdgeSource,
		"dash0intelligentedge",
	)
	return renderResourceTemplate(intelligentEdgeTemplate, values, "dash0intelligentedge")
}

// Dash0IntelligentEdge is cluster-scoped, so the helpers do not take a namespace.

func deployIntelligentEdgeResource(values intelligentEdgeValues) {
	renderedResourceFileName := renderIntelligentEdgeTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf("deploying a Dash0IntelligentEdge resource with values %v", values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func removeIntelligentEdgeResource() {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"Dash0IntelligentEdge",
		intelligentEdgeName,
	))
}
