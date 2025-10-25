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

func swapTag(image ImageSpec, newTag string) ImageSpec {
	return ImageSpec{
		repository: image.repository,
		tag:        newTag,
		pullPolicy: image.pullPolicy,
	}
}
