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

	buildOperatorControllerImageFromLocalSources    = true
	buildInstrumentationImageFromLocalSources       = true
	buildCollectorImageFromLocalSources             = true
	buildConfigurationReloaderImageFromLocalSources = true
	buildFileLogOffsetSynchImageFromLocalSources    = true
)

func rebuildOperatorControllerImage(operatorImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(operatorImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the operator controller image %s, this looks like a remote image",
				renderFullyQualifiedImageName(operatorImage),
			))
		return
	}

	By(fmt.Sprintf("building the operator controller image: %s", renderFullyQualifiedImageName(operatorImage)))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"make",
				"docker-build",
				fmt.Sprintf("CONTROLLER_IMG_REPOSITORY=%s", operatorImage.repository),
				fmt.Sprintf("CONTROLLER_IMG_TAG=%s", operatorImage.tag),
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

func rebuildInstrumentationImage(instrumentationImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(instrumentationImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the instrumenation image %s, this looks like a remote image",
				renderFullyQualifiedImageName(instrumentationImage),
			))
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

func rebuildCollectorImage(collectorImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(collectorImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the collector image %s, this looks like a remote image",
				renderFullyQualifiedImageName(collectorImage),
			))
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
				collectorImage.tag,
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

func rebuildConfigurationReloaderImage(configurationReloaderImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(configurationReloaderImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the configuration reloader image %s, this looks like a remote image",
				renderFullyQualifiedImageName(configurationReloaderImage),
			))
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
				configurationReloaderImage.tag,
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

func rebuildFileLogOffsetSynchImage(fileLogOffsetSynchImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(fileLogOffsetSynchImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the filelog offset synch image %s, this looks like a remote image",
				renderFullyQualifiedImageName(fileLogOffsetSynchImage),
			))
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
				fileLogOffsetSynchImage.tag,
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
