// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func verifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
) {
	By("waiting for the labels to get removed from the workload")
	Eventually(func(g Gomega) {
		verifyNoDash0Labels(g, namespace, runtime, workloadType)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())
}

func verifyLabels(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	successful bool,
	images Images,
	instrumentationBy string,
) {
	instrumented := readLabel(g, namespace, runtime, workloadType, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(strconv.FormatBool(successful)), "expected dash0.com/instrumented=true but it wasn't")
	operatorImage := readLabel(g, namespace, runtime, workloadType, "dash0.com/operator-image")
	verifyImageLabel(g, operatorImage, images.operator, "ghcr.io/dash0hq/operator-controller:")
	initContainerImage := readLabel(g, namespace, runtime, workloadType, "dash0.com/init-container-image")
	verifyImageLabel(g, initContainerImage, images.instrumentation, "ghcr.io/dash0hq/instrumentation:")
	instrumentedBy := readLabel(g, namespace, runtime, workloadType, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(instrumentationBy))
}

func verifyImageLabel(
	g Gomega,
	labelValue string,
	image ImageSpec,
	defaultImageNamePrefix string,
) {
	expectedLabel, expectFullLabel := expectedImageLabel(image, defaultImageNamePrefix)
	if expectFullLabel {
		g.Expect(labelValue).To(Equal(expectedLabel))
	} else {
		g.Expect(labelValue).To(ContainSubstring(expectedLabel))
	}
}

func expectedImageLabel(image ImageSpec, defaultImageName string) (string, bool) {
	// If the repository has been unset explicitly via the respective environment variable, the default image from the
	// helm chart will be used, so we need to test against that.
	if image.repository == "" {
		return util.ImageRefToLabel(defaultImageName), false
	}
	return util.ImageRefToLabel(renderFullyQualifiedImageName(image)), true
}

func renderFullyQualifiedImageName(image ImageSpec) string {
	return fmt.Sprintf("%s:%s", image.repository, image.tag)
}

func verifyNoDash0Labels(g Gomega, namespace string, runtime runtimeType, workloadType workloadType) {
	verifyNoDash0LabelsOrOnlyOptOut(g, namespace, runtime, workloadType, false)
}

func verifyOnlyOptOutLabelIsPresent(g Gomega, namespace string, runtime runtimeType, workloadType workloadType) {
	verifyNoDash0LabelsOrOnlyOptOut(g, namespace, runtime, workloadType, true)
}

func verifyNoDash0LabelsOrOnlyOptOut(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	expectOptOutLabel bool,
) {
	instrumented := readLabel(g, namespace, runtime, workloadType, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(""))
	operatorVersion := readLabel(g, namespace, runtime, workloadType, "dash0.com/operator-image")
	g.Expect(operatorVersion).To(Equal(""))
	initContainerImageVersion := readLabel(g, namespace, runtime, workloadType, "dash0.com/init-container-image")
	g.Expect(initContainerImageVersion).To(Equal(""))
	instrumentedBy := readLabel(g, namespace, runtime, workloadType, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(""))
	dash0Enable := readLabel(g, namespace, runtime, workloadType, "dash0.com/enable")
	if expectOptOutLabel {
		g.Expect(dash0Enable).To(Equal("false"))
	} else {
		g.Expect(dash0Enable).To(Equal(""))
	}
}

func readLabel(g Gomega, namespace string, runtime runtimeType, workloadType workloadType, labelKey string) string {
	labelValue, err := run(exec.Command(
		"kubectl",
		"get",
		workloadType.workloadTypeString,
		"--namespace",
		namespace,
		workloadName(runtime, workloadType),
		"-o",
		fmt.Sprintf("jsonpath={.metadata.labels['%s']}", strings.ReplaceAll(labelKey, ".", "\\.")),
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	return labelValue
}
