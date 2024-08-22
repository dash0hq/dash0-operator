// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"text/template"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

type dash0OperatorConfigurationValues struct {
	Endpoint string
}

const (
	dash0OperatorConfigurationResourceName = "dash0-operator-configuration-resource-e2e"
)

var (
	//go:embed dash0operatorconfiguration.e2e.yaml.template
	dash0OperatorConfigurationResourceSource   string
	dash0OperatorConfigurationResourceTemplate *template.Template

	defaultDash0OperatorConfigurationValues = dash0OperatorConfigurationValues{
		Endpoint: defaultEndpoint,
	}
)

func renderDash0OperatorConfigurationResourceTemplate(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
) string {
	By("render Dash0OperatorConfiguration resource template")
	if dash0OperatorConfigurationResourceTemplate == nil {
		dash0OperatorConfigurationResourceTemplate =
			template.Must(template.New("dash0operatorconfiguration").Parse(dash0OperatorConfigurationResourceSource))
	}

	var dash0OperatorConfigurationResource bytes.Buffer
	Expect(
		dash0OperatorConfigurationResourceTemplate.Execute(
			&dash0OperatorConfigurationResource,
			dash0OperatorConfigurationValues,
		)).To(Succeed())

	renderedResourceFile, err := os.CreateTemp(os.TempDir(), "dash0operatorconfiguration-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(renderedResourceFile.Name(), dash0OperatorConfigurationResource.Bytes(), 0644)).To(Succeed())

	return renderedResourceFile.Name()
}

func deployDash0OperatorConfigurationResource(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
) {
	renderedResourceFileName := renderDash0OperatorConfigurationResourceTemplate(dash0OperatorConfigurationValues)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"Deploying the Dash0 operator configuration resource with values %v", dash0OperatorConfigurationValues))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-f",
			renderedResourceFileName,
		))).To(Succeed())
}

func undeployDash0OperatorConfigurationResource() {
	By("Removing the Dash0 operator configuration resource")
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"dash0operatorconfiguration",
			dash0OperatorConfigurationResourceName,
			"--ignore-not-found",
		))).To(Succeed())
}
