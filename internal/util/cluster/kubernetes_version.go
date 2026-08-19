// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/pager"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"
)

const (
	// nodeListPageSize is the number of nodes the minimum kubelet version detection requests per API server call.
	nodeListPageSize = 100

	// InstrumentAtStartUpDeliverySettleTimeout bounds how long the startup instrumentation waits for the minimum kubelet
	// version detection. See also: minimumKubeletVersionDetectionBackoff.
	InstrumentAtStartUpDeliverySettleTimeout = 95 * time.Second
)

var (
	// minimumKubeletVersionDetectionBackoff governs how often the minimum kubelet version detection is retried after a
	// failure (Steps) and how long it waits in between attempts (Duration), before it gives up for the lifetime of the
	// operator manager process. See also: InstrumentAtStartUpDeliverySettleTimeout.
	minimumKubeletVersionDetectionBackoff = wait.Backoff{
		Duration: 30 * time.Second,
		Factor:   1.0,
		Steps:    3,
		Jitter:   0.1,
	}

	// imageVolumesAutoMinimumVersion is the lowest Kubernetes version where instrumentationDelivery=auto is resolved to
	// image-volume; this is the version in which the feature became stable.
	imageVolumesAutoMinimumVersion = KubernetesVersion{Major: 1, Minor: 36}

	// imageVolumesHardMinimumVersion is the lowest 1.x Kubernetes minor version that supports image volumes at all
	// (1.31), via a feature gate.
	imageVolumesHardMinimumVersion = KubernetesVersion{Major: 1, Minor: 31}

	// trafficDistributionMinimumVersion is the lowest Kubernetes version that enables the service field
	// spec.trafficDistribution by default (1.31, beta; GA in 1.33). 1.30 has the field but its feature gate defaults to
	// false, so the API server prunes it.
	trafficDistributionMinimumVersion = KubernetesVersion{Major: 1, Minor: 31}

	leadingDigitsRegex = regexp.MustCompile(`^[0-9]+`)
)

// KubernetesVersion holds the major and minor version of a Kubernetes component.
type KubernetesVersion struct {
	Major int
	Minor int
}

// IsAtLeast reports whether this version is at least the given version.
func (v KubernetesVersion) IsAtLeast(min KubernetesVersion) bool {
	if v.Major != min.Major {
		return v.Major > min.Major
	}
	return v.Minor >= min.Minor
}

func (v KubernetesVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// KubernetesVersionInfo holds KubernetesVersion (e.g. the major and minor version of a Kubernetes component), with all
// vendor-specific suffixes (like "+" on GKE or "-eks-abc1234" on EKS) removed from the numeric fields, plus the version
// string it was originally parsed from. Depending on the context, the value can refer to the Kubernetes API server
// version (see DetectKubernetesApiServerVersion) or the kubelet version of a node (see MinimumKubeletVersionDetector).
type KubernetesVersionInfo struct {
	// KubernetesVersion the Kubernetes version, if the version has been parsed successfully.
	Version KubernetesVersion

	// The original version string(s) this version was parsed from, if available.
	OriginalVersionString string

	// Detected reports whether the version is actually known: it is false if the version could not be determined (or not
	// yet), and then Version is the zero value. Callers must check Detected before acting on Version, and fall back to
	// behavior that is safe for old Kubernetes versions if it is false.
	Detected bool
}

// IsAtLeast reports whether the version from this KubernetesVersionInfo is at least the given major.minor version. It
// does not take Detected into account, an undetected version is not at least any version.
func (v KubernetesVersionInfo) IsAtLeast(min KubernetesVersion) bool {
	return v.Version.IsAtLeast(min)
}

func (v KubernetesVersionInfo) String() string {
	if v.Detected {
		return v.Version.String()
	} else if v.OriginalVersionString != "" {
		return fmt.Sprintf("? (%s)", v.OriginalVersionString)
	}
	return "?"
}

// DetectKubernetesApiServerVersion reads the Kubernetes API server version of the cluster from
// clientset.Discovery().ServerVersion(). A node can run an older kubelet version than the Kubernetes API server
// version, see MinimumKubeletVersionDetector.
func DetectKubernetesApiServerVersion(
	clientset kubernetes.Interface,
	logger logd.Logger,
) KubernetesVersionInfo {
	if apiServerVersion, err := clientset.Discovery().ServerVersion(); err != nil {
		logger.Error(err, "could not determine the Kubernetes API server version")
		return KubernetesVersionInfo{}
	} else {
		return parseKubernetesApiServerVersion(apiServerVersion, logger)
	}
}

// MinimumKubeletVersionDetector determines the minimum kubelet version among all nodes of the cluster. A node can lag
// behind the Kubernetes API server version, so features that need support from the kubelet (image volumes, for example)
// must not be enabled based on the Kubernetes API server version alone.
//
// Clusters can have a lot of nodes, therefore the detection runs in a background goroutine instead of blocking the
// operator manager startup. Until it has finished, Get reports the minimum kubelet version as unknown, and callers are
// expected to fall back to the behavior for old Kubernetes versions.
//
// The nodes are inspected exactly once at startup. Nodes joining the cluster later are not taken into account.
type MinimumKubeletVersionDetector struct {
	minimumKubeletVersion atomic.Pointer[KubernetesVersionInfo]

	// settled is closed when the detection has come to an end, that is, when it has either determined the minimum
	// kubelet version or given up permanently.
	settled     chan struct{}
	settledOnce sync.Once
}

// NewMinimumKubeletVersionDetector creates a detector whose minimum kubelet version is unknown until StartDetection
// has been called and the detection has finished.
func NewMinimumKubeletVersionDetector() *MinimumKubeletVersionDetector {
	return &MinimumKubeletVersionDetector{settled: make(chan struct{})}
}

// Get returns the minimum kubelet version among all nodes of the cluster. Its Detected flag is false while the
// detection is still in progress, as well as when it has failed permanently. See WaitForDetection for a variant that
// blocks until the detection is done.
func (d *MinimumKubeletVersionDetector) Get() KubernetesVersionInfo {
	if d == nil {
		return KubernetesVersionInfo{}
	}
	if minimumKubeletVersion := d.minimumKubeletVersion.Load(); minimumKubeletVersion != nil {
		return *minimumKubeletVersion
	}
	return KubernetesVersionInfo{}
}

// WaitForDetection blocks until the minimum kubelet version detection has settled, that is, until it has either
// determined the version or given up permanently, and returns the result. It stops waiting when the given timeout
// elapses or when ctx is done, and then reports the version as undetected. Callers must check the Detected flag either
// way and fall back to behavior that is safe for old Kubernetes versions if it is false.
func (d *MinimumKubeletVersionDetector) WaitForDetection(
	ctx context.Context,
	timeout time.Duration,
) KubernetesVersionInfo {
	if d == nil {
		return KubernetesVersionInfo{}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-d.settled:
	case <-timer.C:
	case <-ctx.Done():
	}
	return d.Get()
}

// StartDetection inspects all nodes of the cluster in a background goroutine and records the minimum kubelet version
// it finds, so the detection does not delay the operator manager startup. The optional onDetected callback is invoked
// once the minimum kubelet version is available.
func (d *MinimumKubeletVersionDetector) StartDetection(
	ctx context.Context,
	clientset kubernetes.Interface,
	onDetected func(),
	logger logd.Logger,
) {
	go func() {
		defer d.settledOnce.Do(func() { close(d.settled) })
		if err := retry.RetryWithCustomBackoff(
			"determining the minimum kubelet version of the cluster's nodes",
			func() error {
				if ctx.Err() != nil {
					// the operator manager is shutting down, stop retrying
					return retry.NewRetryableError(ctx.Err(), false)
				}
				minimumKubeletVersion, err := detectMinimumKubeletVersion(ctx, clientset, logger)
				if err != nil {
					return err
				}
				d.minimumKubeletVersion.Store(&minimumKubeletVersion)
				return nil
			},
			minimumKubeletVersionDetectionBackoff,
			true,
			false,
			logger,
		); err != nil {
			if ctx.Err() == nil {
				logger.ErrorAsWarnTelemetryCollectionIssue(err,
					"The minimum kubelet version of nodes in the cluster could not be determined. "+
						"Features that require a minimum kubelet version on all nodes will remain disabled.")
			}
			return
		}
		logger.Info(
			fmt.Sprintf("The minimum kubelet version of all nodes in the cluster has been detected as %s.", d.Get()),
		)
		if onDetected != nil {
			onDetected()
		}
	}()
}

// detectMinimumKubeletVersion lists all nodes of the cluster and returns the minimum kubelet version among them. It
// returns an error if the nodes could not be listed, or if the kubelet version of at least one node could not be
// parsed: only knowing the kubelet version of a subset of the nodes is not enough to conclude that all nodes have a
// certain minimum kubelet version. An unparseable kubelet version is reported as a non-retryable error, retrying would
// not produce a different result.
func detectMinimumKubeletVersion(
	ctx context.Context,
	clientset kubernetes.Interface,
	logger logd.Logger,
) (KubernetesVersionInfo, error) {
	// The nodes are fetched in chunks, since a node object is fairly large (its status carries the list of all images
	// present on the node) and a cluster can have a lot of nodes. This uses client-go's pager directly; the
	// controller-runtime cache-backed clients do not support pagination.
	pgr := pager.New(pager.SimplePageFunc(
		func(opts metav1.ListOptions) (runtime.Object, error) {
			return clientset.CoreV1().Nodes().List(ctx, opts)
		},
	))
	pgr.PageSize = nodeListPageSize

	var minimumKubeletVersion *KubernetesVersionInfo
	if err := pgr.EachListItem(ctx, metav1.ListOptions{}, func(object runtime.Object) error {
		node, isNode := object.(*corev1.Node)
		if !isNode {
			return retry.NewRetryableError(fmt.Errorf("expected a node, but got %T", object), false)
		}
		kubeletVersionInfo, err := parseKubeletVersion(node.Status.NodeInfo.KubeletVersion, logger)
		if err != nil {
			return retry.NewRetryableError(
				fmt.Errorf("cannot parse the kubelet version of node %s: %w", node.Name, err), false)
		}
		if minimumKubeletVersion == nil || !kubeletVersionInfo.IsAtLeast(minimumKubeletVersion.Version) {
			minimumKubeletVersion = &kubeletVersionInfo
		}
		return nil
	}); err != nil {
		return KubernetesVersionInfo{}, err
	}
	if minimumKubeletVersion == nil {
		return KubernetesVersionInfo{}, fmt.Errorf("the cluster has no nodes")
	}
	return *minimumKubeletVersion, nil
}

func parseKubernetesApiServerVersion(apiServerVersion *version.Info, logger logd.Logger) KubernetesVersionInfo {
	logger.Debug("Kubernetes API server version", "version info", apiServerVersion)
	major, majorErr := extractLeadingDigits(apiServerVersion.Major)
	minor, minorErr := extractLeadingDigits(apiServerVersion.Minor)
	versionString := fmt.Sprintf("%s.%s", apiServerVersion.Major, apiServerVersion.Minor)
	if majorErr != nil || minorErr != nil {
		logger.Error(
			fmt.Errorf("could not parse Kubernetes API server version major=%q minor=%q; errors: %v, %v",
				apiServerVersion.Major, apiServerVersion.Minor, majorErr, minorErr),
			"could not parse the Kubernetes API server version",
		)
		return KubernetesVersionInfo{OriginalVersionString: versionString}
	}
	logger.Debug("Kubernetes API server version parsed as",
		"major", major, "minor", minor, "versionString", versionString)
	return KubernetesVersionInfo{
		Version:               KubernetesVersion{Major: major, Minor: minor},
		OriginalVersionString: versionString,
		Detected:              true,
	}
}

// parseKubeletVersion parses the kubelet version reported in a node's status.nodeInfo.kubeletVersion, for example
// "v1.36.0", "v1.31.5-gke.1000" or "v1.30.14-eks-3abbec1". Only the major and minor version are retained, the patch
// version and any vendor suffix are discarded.
func parseKubeletVersion(kubeletVersion string, logger logd.Logger) (KubernetesVersionInfo, error) {
	logger.Debug("kubelet version", "version", kubeletVersion)
	segments := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(kubeletVersion), "v"), ".", 3)
	if len(segments) < 2 {
		return KubernetesVersionInfo{
			OriginalVersionString: kubeletVersion,
		}, fmt.Errorf("cannot parse kubelet version %q", kubeletVersion)
	}
	major, majorErr := extractLeadingDigits(segments[0])
	minor, minorErr := extractLeadingDigits(segments[1])
	if majorErr != nil || minorErr != nil {
		return KubernetesVersionInfo{
			OriginalVersionString: kubeletVersion,
		}, fmt.Errorf("cannot parse kubelet version %q; errors: %v, %v", kubeletVersion, majorErr, minorErr)
	}
	return KubernetesVersionInfo{
		Version:               KubernetesVersion{Major: major, Minor: minor},
		Detected:              true,
		OriginalVersionString: kubeletVersion,
	}, nil
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
