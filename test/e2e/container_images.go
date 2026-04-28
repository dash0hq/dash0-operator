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
	intelligentEdgeCollector     ImageSpec
	barker                       ImageSpec
}

const (
	operatorControllerImageName           = "operator-controller"
	instrumentationImageName              = "instrumentation"
	collectorImageName                    = "collector"
	configurationReloaderImageName        = "configuration-reloader"
	filelogOffsetSyncImageName            = "filelog-offset-sync"
	filelogOffsetVolumeOwnershipImageName = "filelog-offset-volume-ownership"
	targetAllocatorImageName              = "target-allocator"
	intelligentEdgeCollectorImageName     = "intelligent-edge-collector"
	barkerImageName                       = "barker"

	tagLatest = "latest"

	productionImageRepositoryPrefix = "ghcr.io/dash0hq/"
	defaultImageRepositoryPrefix    = ""
	defaultImageTag                 = tagLatest
	defaultPullPolicy               = "Always"
)

var (
	images Images
)

func determineContainerImages() {
	if !isLocalHelmChart(operatorHelmChart) {
		images = Images{}
		e2ePrint("Using a non-local Helm chart (%s). Image settings will come from the chart. "+
			"Ignoring IMAGE_REPOSITORY_PREFIX, IMAGE_TAG, PULL_POLICY, and per-image environment variables.\n",
			operatorHelmChart)
		return
	}

	repositoryPrefix := getEnvOrDefault("IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix)
	imageTag := getEnvOrDefault("IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("PULL_POLICY", defaultPullPolicy)
	images = createContainerImages(repositoryPrefix, imageTag, pullPolicy)
}

func createContainerImagesForHelmChartVersion(version string) Images {
	return createContainerImages(productionImageRepositoryPrefix, version, "")
}

func createContainerImages(repositoryPrefix string, imageTag string, pullPolicy string) Images {
	return Images{
		operator: determineContainerImage(
			"CONTROLLER",
			repositoryPrefix,
			operatorControllerImageName,
			imageTag,
			pullPolicy,
		),
		instrumentation: determineContainerImage(
			"INSTRUMENTATION",
			repositoryPrefix,
			instrumentationImageName,
			imageTag,
			pullPolicy,
		),
		collector: determineContainerImage(
			"COLLECTOR",
			repositoryPrefix,
			collectorImageName,
			imageTag,
			pullPolicy,
		),
		configurationReloader: determineContainerImage(
			"CONFIGURATION_RELOADER",
			repositoryPrefix,
			configurationReloaderImageName,
			imageTag,
			pullPolicy,
		),
		fileLogOffsetSync: determineContainerImage(
			"FILELOG_OFFSET_SYNC",
			repositoryPrefix,
			filelogOffsetSyncImageName,
			imageTag,
			pullPolicy,
		),
		fileLogOffsetVolumeOwnership: determineContainerImage(
			"FILELOG_OFFSET_VOLUME_OWNERSHIP",
			repositoryPrefix,
			filelogOffsetVolumeOwnershipImageName,
			imageTag,
			pullPolicy,
		),
		targetAllocator: determineContainerImage(
			"TARGET_ALLOCATOR",
			repositoryPrefix,
			targetAllocatorImageName,
			imageTag,
			pullPolicy,
		),
		intelligentEdgeCollector: determineContainerImage(
			"INTELLIGENT_EDGE_COLLECTOR",
			repositoryPrefix,
			intelligentEdgeCollectorImageName,
			imageTag,
			pullPolicy,
		),
		barker: determineContainerImage(
			"BARKER",
			repositoryPrefix,
			barkerImageName,
			imageTag,
			pullPolicy,
		),
	}
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
