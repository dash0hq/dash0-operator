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
	repository    string
	tag           string
	digest        string
	pullPolicy    string
	dockerContext string
	dockerfile    string
}

type Images struct {
	operator                     ImageSpec
	instrumentation              ImageSpec
	collector                    ImageSpec
	configurationReloader        ImageSpec
	fileLogOffsetSync            ImageSpec
	fileLogOffsetVolumeOwnership ImageSpec
}

const (
	tagLatest          = "latest"
	tagMainDev         = "main-dev"
	additionalImageTag = "e2e-test"
)

var (
	localImages = Images{
		operator: ImageSpec{
			repository:    "operator-controller",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: ".",
		},
		instrumentation: ImageSpec{
			repository:    "instrumentation",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: "images/instrumentation",
		},
		collector: ImageSpec{
			repository:    "collector",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: "images/collector",
		},
		configurationReloader: ImageSpec{
			repository:    "configuration-reloader",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: "images",
			dockerfile:    "images/configreloader/Dockerfile",
		},
		fileLogOffsetSync: ImageSpec{
			repository:    "filelog-offset-sync",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: "images",
			dockerfile:    "images/filelogoffsetsync/Dockerfile",
		},
		fileLogOffsetVolumeOwnership: ImageSpec{
			repository:    "filelog-offset-volume-ownership",
			tag:           tagLatest,
			pullPolicy:    "Never",
			dockerContext: "images",
			dockerfile:    "images/filelogoffsetvolumeownership/Dockerfile",
		},
	}

	emptyImages = Images{
		operator: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.operator.dockerContext,
			dockerfile:    localImages.operator.dockerfile,
		},
		instrumentation: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.instrumentation.dockerContext,
			dockerfile:    localImages.instrumentation.dockerfile,
		},
		collector: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.collector.dockerContext,
			dockerfile:    localImages.collector.dockerfile,
		},
		configurationReloader: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.configurationReloader.dockerContext,
			dockerfile:    localImages.configurationReloader.dockerfile,
		},
		fileLogOffsetSync: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.fileLogOffsetSync.dockerContext,
			dockerfile:    localImages.fileLogOffsetSync.dockerfile,
		},
		fileLogOffsetVolumeOwnership: ImageSpec{
			repository:    "",
			tag:           "",
			pullPolicy:    "",
			dockerContext: localImages.fileLogOffsetVolumeOwnership.dockerContext,
			dockerfile:    localImages.fileLogOffsetVolumeOwnership.dockerfile,
		},
	}

	images = localImages
)

func rebuildAllContainerImages() {
	rebuildLocalImage(images.operator)
	rebuildLocalImage(images.instrumentation)
	rebuildLocalImage(images.collector)
	rebuildLocalImage(images.configurationReloader)
	rebuildLocalImage(images.fileLogOffsetSync)
	rebuildLocalImage(images.fileLogOffsetVolumeOwnership)
}

func rebuildLocalImage(imageSpec ImageSpec) {
	if !shouldBuildImageLocally(imageSpec) {
		return
	}

	By(
		fmt.Sprintf(
			"building the %s image: %s",
			imageSpec.repository,
			renderFullyQualifiedImageName(imageSpec),
		))
	dockerfile := imageSpec.dockerfile
	if dockerfile == "" {
		dockerfile = fmt.Sprintf("%s/%s", imageSpec.dockerContext, "Dockerfile")
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"-f",
				dockerfile,
				"-t",
				fmt.Sprintf("%s:%s", imageSpec.repository, imageSpec.tag),
				imageSpec.dockerContext,
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: imageSpec.repository,
		tag:        additionalImageTag,
	}
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(imageSpec),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())

	loadImageToKindClusterIfRequired(imageSpec, &additionalTag)
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
