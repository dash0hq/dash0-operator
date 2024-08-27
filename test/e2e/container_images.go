// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

type ImageSpec struct {
	repository string
	tag        string
	digest     string
	pullPolicy string
}

type Images struct {
	operator              ImageSpec
	instrumentation       ImageSpec
	collector             ImageSpec
	configurationReloader ImageSpec
	fileLogOffsetSynch    ImageSpec
}

const (
	additionalImageTag = "e2e-test"
)

var (
	images = Images{
		operator: ImageSpec{
			repository: "operator-controller",
			tag:        "latest",
			pullPolicy: "Never",
		},
		instrumentation: ImageSpec{
			repository: "instrumentation",
			tag:        "latest",
			pullPolicy: "Never",
		},
		collector: ImageSpec{
			repository: "collector",
			tag:        "latest",
			pullPolicy: "Never",
		},
		configurationReloader: ImageSpec{
			repository: "configuration-reloader",
			tag:        "latest",
			pullPolicy: "Never",
		},
		fileLogOffsetSynch: ImageSpec{
			repository: "filelog-offset-synch",
			tag:        "latest",
			pullPolicy: "Never",
		},
	}
)

func rebuildAllContainerImages() {
	rebuildOperatorControllerImage(images.operator)
	rebuildInstrumentationImage(images.instrumentation)
	rebuildCollectorImage(images.collector)
	rebuildConfigurationReloaderImage(images.configurationReloader)
	rebuildFileLogOffsetSynchImage(images.fileLogOffsetSynch)
}

func rebuildOperatorControllerImage(operatorImage ImageSpec) {
	if !shouldBuildImageLocally(operatorImage) {
		return
	}

	By(fmt.Sprintf("building the operator controller image: %s", renderFullyQualifiedImageName(operatorImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				".",
				"-t",
				fmt.Sprintf("%s:%s", operatorImage.repository, operatorImage.tag),
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: operatorImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(operatorImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func rebuildInstrumentationImage(instrumentationImage ImageSpec) {
	if !shouldBuildImageLocally(instrumentationImage) {
		return
	}

	By(fmt.Sprintf("building the instrumentation image: %s", renderFullyQualifiedImageName(instrumentationImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"images/instrumentation/build.sh",
				instrumentationImage.repository,
				instrumentationImage.tag,
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: instrumentationImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(instrumentationImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func rebuildCollectorImage(collectorImage ImageSpec) {
	if !shouldBuildImageLocally(collectorImage) {
		return
	}

	By(fmt.Sprintf("building the collector image: %s", renderFullyQualifiedImageName(collectorImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"images/collector",
				"-t",
				fmt.Sprintf("%s:%s", collectorImage.repository, collectorImage.tag),
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: collectorImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(collectorImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func rebuildConfigurationReloaderImage(configurationReloaderImage ImageSpec) {
	if !shouldBuildImageLocally(configurationReloaderImage) {
		return
	}

	By(fmt.Sprintf("building the configuration reloader image: %s",
		renderFullyQualifiedImageName(configurationReloaderImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"images/configreloader",
				"-t",
				fmt.Sprintf("%s:%s", configurationReloaderImage.repository, configurationReloaderImage.tag),
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: configurationReloaderImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(configurationReloaderImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func rebuildFileLogOffsetSynchImage(fileLogOffsetSynchImage ImageSpec) {
	if !shouldBuildImageLocally(fileLogOffsetSynchImage) {
		return
	}

	By(fmt.Sprintf("building the filelog offset synch image: %s",
		renderFullyQualifiedImageName(fileLogOffsetSynchImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"images/filelogoffsetsynch",
				"-t",
				fmt.Sprintf("%s:%s", fileLogOffsetSynchImage.repository, fileLogOffsetSynchImage.tag),
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: fileLogOffsetSynchImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(fileLogOffsetSynchImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func shouldBuildImageLocally(image ImageSpec) bool {
	if strings.Contains(image.repository, "/") {
		By(fmt.Sprintf(
			"not rebuilding the image %s, this looks like a remote image", renderFullyQualifiedImageName(image),
		))
		return false
	}

	if image.repository == "" && operatorHelmChartUrl != "" {
		By("not rebuilding image, a remote Helm chart is used with the default image from the chart")
		return false
	}

	return true
}
