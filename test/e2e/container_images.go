// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
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
	targetAllocator              ImageSpec
}

const (
	operatorControllerImageName           = "operator-controller"
	instrumentationImageName              = "instrumentation"
	collectorImageName                    = "collector"
	configurationReloaderImageName        = "configuration-reloader"
	filelogOffsetSyncImageName            = "filelog-offset-sync"
	filelogOffsetVolumeOwnershipImageName = "filelog-offset-volume-ownership"
	targetAllocatorImageName              = "target-allocator"

	tagLatest  = "latest"
	tagMainDev = "main-dev"

	productionImageRepositoryPrefix = "ghcr.io/dash0hq/"
	defaultImageRepositoryPrefix    = ""
	defaultImageTag                 = tagLatest
	defaultPullPolicy               = "Always"
)

var (
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
		targetAllocator: ImageSpec{
			repository: "",
			tag:        "",
			pullPolicy: "",
		},
	}

	images Images
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
			operatorControllerImageName,
			imageTag,
			pullPolicy,
		)
	images.instrumentation =
		determineContainerImage(
			"INSTRUMENTATION",
			repositoryPrefix,
			instrumentationImageName,
			imageTag,
			pullPolicy,
		)
	images.collector =
		determineContainerImage(
			"COLLECTOR",
			repositoryPrefix,
			collectorImageName,
			imageTag,
			pullPolicy,
		)
	images.configurationReloader =
		determineContainerImage(
			"CONFIGURATION_RELOADER",
			repositoryPrefix,
			configurationReloaderImageName,
			imageTag,
			pullPolicy,
		)
	images.fileLogOffsetSync =
		determineContainerImage(
			"FILELOG_OFFSET_SYNC",
			repositoryPrefix,
			filelogOffsetSyncImageName,
			imageTag,
			pullPolicy,
		)
	images.fileLogOffsetVolumeOwnership =
		determineContainerImage(
			"FILELOG_OFFSET_VOLUME_OWNERSHIP",
			repositoryPrefix,
			filelogOffsetVolumeOwnershipImageName,
			imageTag,
			pullPolicy,
		)
	images.targetAllocator =
		determineContainerImage(
			"TARGET_ALLOCATOR",
			repositoryPrefix,
			targetAllocatorImageName,
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

func deriveAlternativeImagesForUpdateTest(images Images) Images {
	return Images{
		// Deliberately not using a different image for the operator manager itself. Replacing the operator manager
		// image with say, ghcr.io/dash0hq/operator-controller:latest (i.e., the most recently published release) or
		// ghcr.io/dash0hq/operator-controller:main-dev (the most recent build on main) would lead to the operator
		// manager crashing at startup with "flag provided but not defined: -new-command-line-argument" if the current
		// branch adds new command line flags (which is of course unknown in those operator manager image versions).
		operator: images.operator,
		instrumentation: deriveAlternativeImageForUpdateTest(
			images.instrumentation,
			instrumentationImageName,
		),
		// Deliberately not using a different image for the collector. Replacing the collector image with say,
		// ghcr.io/dash0hq/collector:latest (i.e., the most recently published release) but combining this with the config
		// maps generated by the current operator (because we always use the locally built operator) can lead to invalid
		// combinations; that is, config maps that are not valid for the collector version we run.
		collector: images.collector,
		configurationReloader: deriveAlternativeImageForUpdateTest(
			images.configurationReloader,
			configurationReloaderImageName,
		),
		fileLogOffsetSync: deriveAlternativeImageForUpdateTest(
			images.fileLogOffsetSync,
			filelogOffsetSyncImageName,
		),
		fileLogOffsetVolumeOwnership: deriveAlternativeImageForUpdateTest(
			images.fileLogOffsetVolumeOwnership,
			filelogOffsetVolumeOwnershipImageName,
		),
		targetAllocator: deriveAlternativeImageForUpdateTest(
			images.targetAllocator,
			targetAllocatorImageName,
		),
	}
}

func deriveAlternativeImageForUpdateTest(image ImageSpec, imageName string) ImageSpec {
	productionImage := productionImageRepositoryPrefix + imageName
	// For the "should update instrumentation modifications at startup" test case, we need to come up with an
	// alternative fully qualified image name, one that is different from the image name that is being used for the
	// regular "helm install" invocations in this e2e test suite run.
	if image.repository != productionImage || image.tag != tagLatest {
		// Use "ghcr.io/dash0hq/$image:latest", if that is different from the original image.
		// (The "latest" tag is built with every release, although it is not used in the Helm chart.)
		return ImageSpec{
			repository: productionImage,
			tag:        tagLatest,
		}
	} else {
		// Otherwise, as a fallback, use "ghcr.io/dash0hq/$image:main-dev".
		// (The "main-dev" tag is built for every commit pushed to the main branch.)
		return ImageSpec{
			repository: productionImage,
			tag:        tagMainDev,
		}
	}
}
