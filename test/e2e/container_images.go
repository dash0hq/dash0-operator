// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
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
	tagLatest          = "latest"
	tagMainDev         = "main-dev"
	additionalImageTag = "e2e-test"
)

var (
	images = Images{
		operator: ImageSpec{
			repository: "operator-controller",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
		instrumentation: ImageSpec{
			repository: "instrumentation",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
		collector: ImageSpec{
			repository: "collector",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
		configurationReloader: ImageSpec{
			repository: "configuration-reloader",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
		fileLogOffsetSynch: ImageSpec{
			repository: "filelog-offset-synch",
			tag:        tagLatest,
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
				"-t",
				fmt.Sprintf("%s:%s", operatorImage.repository, operatorImage.tag),
				".",
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

	loadImageToKindClusterIfRequired(operatorImage, &additionalTag)
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

	loadImageToKindClusterIfRequired(instrumentationImage, &additionalTag)
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
				"-t",
				fmt.Sprintf("%s:%s", collectorImage.repository, collectorImage.tag),
				"images/collector",
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

	loadImageToKindClusterIfRequired(collectorImage, &additionalTag)
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
				"-f",
				"images/configreloader/Dockerfile",
				"-t",
				fmt.Sprintf("%s:%s", configurationReloaderImage.repository, configurationReloaderImage.tag),
				"images",
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

	loadImageToKindClusterIfRequired(configurationReloaderImage, &additionalTag)
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
				"-f",
				"images/filelogoffsetsynch/Dockerfile",
				"-t",
				fmt.Sprintf("%s:%s", fileLogOffsetSynchImage.repository, fileLogOffsetSynchImage.tag),
				"images",
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

	loadImageToKindClusterIfRequired(fileLogOffsetSynchImage, &additionalTag)
}

func shouldBuildImageLocally(image ImageSpec) bool {
	if isRemoteImage(image) {
		By(fmt.Sprintf(
			"not rebuilding the image %s, this looks like a remote image", renderFullyQualifiedImageName(image),
		))
		return false
	}
	if isDefaultImageFromHelmChart(image) {
		By("not rebuilding image, a remote Helm chart is used with the default image from the chart")
		return false
	}
	return true
}

func loadImageToKindClusterIfRequired(image ImageSpec, additionalTag *ImageSpec) {
	if !isKindCluster() {
		return
	}

	bothImages := []ImageSpec{image}
	if additionalTag != nil {
		bothImages = append(bothImages, *additionalTag)
	}
	for _, img := range bothImages {
		By(fmt.Sprintf("loading the image %s into the kind cluster %s", renderFullyQualifiedImageName(img), kindClusterName))
		err := runAndIgnoreOutput(
			exec.Command(
				"kind",
				"load",
				"docker-image",
				"--name",
				kindClusterName,
				renderFullyQualifiedImageName(img),
			))
		Expect(err).ToNot(HaveOccurred())
	}
}

func isRemoteImage(image ImageSpec) bool {
	return strings.Contains(image.repository, "/")
}

func isDefaultImageFromHelmChart(image ImageSpec) bool {
	return image.repository == "" && operatorHelmChartUrl != ""
}

func deriveAlternativeImageForUpdateTest(image ImageSpec) ImageSpec {
	// For the "should update instrumentation modifications at startup" test case, we need to come up with
	// an alternative image name to the one that is being used for the tests. When testing with the remote Helm
	// chart, we usually use the default images from the chart by setting the image repository and tag to an
	// empty string via the environment variables. The easiest way to come up with an alternative image name is
	// to use "latest" instead of the actual version tag.

	if isDefaultImageFromHelmChart(image) || isRemoteImage(image) {
		alternativeImageTag := tagLatest
		if image.tag == tagLatest {
			// make sure the tag is a different one than the original from `image`.
			image.tag = tagMainDev
		}
		return ImageSpec{
			repository: image.repository,
			tag:        alternativeImageTag,
			pullPolicy: image.pullPolicy,
		}
	} else {
		alternativeImageTag := additionalImageTag
		if image.tag == additionalImageTag {
			// make sure the tag is a different one than the original from `image`.
			image.tag = tagLatest
		}
		return ImageSpec{
			repository: image.repository,
			tag:        alternativeImageTag,
			pullPolicy: image.pullPolicy,
		}
	}
}
