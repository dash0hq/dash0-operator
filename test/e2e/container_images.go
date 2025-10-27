// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"

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
	operator                     ImageSpec
	instrumentation              ImageSpec
	collector                    ImageSpec
	configurationReloader        ImageSpec
	fileLogOffsetSync            ImageSpec
	fileLogOffsetVolumeOwnership ImageSpec
}

const (
	tagLatest                    = "latest"
	updateTestAdditionalImageTag = "e2e-test"

	defaultImageRepositoryPrefix = ""
	defaultImageTag              = tagLatest
	defaultPullPolicy            = "Never"
)

var (
	localImages = Images{
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
		fileLogOffsetSync: ImageSpec{
			repository: "filelog-offset-sync",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
		fileLogOffsetVolumeOwnership: ImageSpec{
			repository: "filelog-offset-volume-ownership",
			tag:        tagLatest,
			pullPolicy: "Never",
		},
	}

	emptyImages = Images{
		operator: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
		instrumentation: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
		collector: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
		configurationReloader: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
		fileLogOffsetSync: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
		fileLogOffsetVolumeOwnership: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
	}

	images = localImages
)

func determineContainerImages() {
	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)

	if !isLocalHelmChart() {
		images = emptyImages
		e2ePrint("Using a non-local Helm chart (%s). Image settings will come from the chart. "+
			"Ignoring IMAGE_REPOSITORY_PREFIX, IMAGE_TAG, PULL_POLICY, and per-image environment variables.\n",
			operatorHelmChart)
		return
	}

	repositoryPrefix := getEnvOrDefault("IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix)
	imageTag := getEnvOrDefault("IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("PULL_POLICY", defaultPullPolicy)

	images.operator =
		determineContainerImage(
			"CONTROLLER",
			repositoryPrefix,
			"operator-controller",
			imageTag,
			pullPolicy,
		)
	images.instrumentation =
		determineContainerImage(
			"INSTRUMENTATION",
			repositoryPrefix,
			"instrumentation",
			imageTag,
			pullPolicy,
		)
	images.collector =
		determineContainerImage(
			"COLLECTOR",
			repositoryPrefix,
			"collector",
			imageTag,
			pullPolicy,
		)
	images.configurationReloader =
		determineContainerImage(
			"CONFIGURATION_RELOADER",
			repositoryPrefix,
			"configuration-reloader",
			imageTag,
			pullPolicy,
		)
	images.fileLogOffsetSync =
		determineContainerImage(
			"FILELOG_OFFSET_SYNC",
			repositoryPrefix,
			"filelog-offset-sync",
			imageTag,
			pullPolicy,
		)
	images.fileLogOffsetVolumeOwnership =
		determineContainerImage(
			"FILELOG_OFFSET_VOLUME_OWNERSHIP",
			repositoryPrefix,
			"filelog-offset-volume-ownership",
			imageTag,
			pullPolicy,
		)
}

func determineContainerImage(
	envVarPrefix string,
	repositoryPrefix string,
	imageName string,
	imageTag string,
	pullPolicy string,
) ImageSpec {
	imageRepository := fmt.Sprintf("%s%s", repositoryPrefix, imageName)
	return ImageSpec{
		repository: getEnvOrDefault(fmt.Sprintf("%s_IMAGE_REPOSITORY", envVarPrefix), imageRepository),
		tag:        getEnvOrDefault(fmt.Sprintf("%s_IMAGE_TAG", envVarPrefix), imageTag),
		digest:     getEnvOrDefault(fmt.Sprintf("%s_IMAGE_DIGEST", envVarPrefix), ""),
		pullPolicy: getEnvOrDefault(fmt.Sprintf("%s_IMAGE_PULL_POLICY", envVarPrefix), pullPolicy),
	}
}

func getEnvOrDefault(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)
	if isSet {
		return value
	}
	return defaultValue
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

}

func swapTag(image ImageSpec, newTag string) ImageSpec {
	return ImageSpec{
		repository: image.repository,
		tag:        newTag,
		pullPolicy: image.pullPolicy,
	}
}
