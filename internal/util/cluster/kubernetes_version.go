// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"
	"regexp"
	"strconv"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	// imageVolumesAutoMinimumMinorVersion is the lowest Kubernetes major version where image volumes are stable.
	// Used together with imageVolumesAutoMinimumMinorVersion.
	imageVolumesAutoMinimumMajorVersion = 1

	// imageVolumesAutoMinimumMinorVersion is the lowest 1.x Kubernetes minor version where image volumes are stable
	// (1.36), and where instrumentationDelivery=auto resolves to image volumes. Used together with
	// imageVolumesAutoMinimumMajorVersion.
	imageVolumesAutoMinimumMinorVersion = 36

	// imageVolumesAlwaysMinimumMinorVersion is the lowest 1.x Kubernetes minor version that supports image volumes at
	// all (1.31).
	imageVolumesAlwaysMinimumMinorVersion = 31
)

var leadingDigitsRegex = regexp.MustCompile(`^[0-9]+`)

type KubernetesVersionInfo struct {
	Major         int
	Minor         int
	VersionString string
}

// DetectKubernetesVersion reads the Kubernetes version of the cluster from clientset.Discovery().ServerVersion().
func DetectKubernetesVersion(clientset *kubernetes.Clientset, logger logd.Logger) (KubernetesVersionInfo, bool) {
	if serverVersion, err := clientset.Discovery().ServerVersion(); err != nil {
		logger.Error(err, "could not determine the Kubernetes version")
		return KubernetesVersionInfo{Major: 0, Minor: 0, VersionString: ""}, false
	} else {
		return parseKubernetesVersion(serverVersion, logger)
	}
}

func parseKubernetesVersion(serverVersion *version.Info, logger logd.Logger) (KubernetesVersionInfo, bool) {
	logger.Debug("Kubernetes version", "version info", serverVersion)
	major, majorErr := extractLeadingDigits(serverVersion.Major)
	minor, minorErr := extractLeadingDigits(serverVersion.Minor)
	versionString := fmt.Sprintf("%s.%s", serverVersion.Major, serverVersion.Minor)
	if majorErr != nil || minorErr != nil {
		logger.Error(
			fmt.Errorf("could not parse Kubernetes version major=%q minor=%q", serverVersion.Major, serverVersion.Minor),
			"could not parse the Kubernetes version",
		)
		return KubernetesVersionInfo{Major: 0, Minor: 0, VersionString: versionString}, false
	}
	logger.Debug("Kubernetes version parsed as", "major", major, "minor", minor, "versionString", versionString)
	return KubernetesVersionInfo{Major: major, Minor: minor, VersionString: versionString}, true
}

// extractLeadingDigits extracts and returns the integer formed by the longest prefix of s that consists only of ASCII
// digits. Characters from the first non-digit byte onward are ignored. If the string starts with a non-digit or is
// empty, it returns an error.
func extractLeadingDigits(s string) (int, error) {
	match := leadingDigitsRegex.FindString(s)
	if match == "" {
		return 0, fmt.Errorf("no leading digits in %q", s)
	}
	return strconv.Atoi(match)
}
