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

const signalControlName = "dash0-signal-control"

type signalControlValues struct {
	DecisionMakerEndpoint   string
	ControlPlaneApiEndpoint string
}

var (
	//go:embed dash0signalcontrol.e2e.yaml.template
	signalControlSource   string
	signalControlTemplate *template.Template
)

func renderSignalControlTemplate(values signalControlValues) string {
	signalControlTemplate = initTemplateOnce(
		signalControlTemplate,
		signalControlSource,
		"dash0signalcontrol",
	)
	return renderResourceTemplate(signalControlTemplate, values, "dash0signalcontrol")
}

// Dash0SignalControl is cluster-scoped, so the helpers do not take a namespace.

func deploySignalControlResource(values signalControlValues) {
	renderedResourceFileName := renderSignalControlTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf("deploying a Dash0SignalControl resource with values %v", values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func removeSignalControlResource() {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"Dash0SignalControl",
		signalControlName,
	))
}
