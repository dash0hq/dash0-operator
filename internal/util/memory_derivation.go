// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const mibBytes = 1024 * 1024

const (
	// GoMemLimitDefaultPercent is the GOMEMLIMIT-to-memory-limit ratio for the plain Go containers.
	GoMemLimitDefaultPercent = 80
	// Agent0ConnectorGoMemLimitPercent is lower than the default because the agent0-connector also runs kubectl as a
	// child process, whose memory counts towards the container limit but is not governed by GOMEMLIMIT.
	Agent0ConnectorGoMemLimitPercent = 60
	// goMemLimitMinHeadroomMiB is the minimum absolute memory left below the limit (limit - GOMEMLIMIT); it covers the
	// parts of RSS that GOMEMLIMIT does not account for (the binary's own mappings, delayed return-to-OS).
	goMemLimitMinHeadroomMiB = 8
)

// CollectorMemorySettings holds the memory_limiter thresholds and the GOMEMLIMIT value derived from a
// collector container's memory limit. See DeriveCollectorMemorySettings for the formula.
type CollectorMemorySettings struct {
	// GoMemLimit is the GOMEMLIMIT value, formatted for the Go runtime (e.g. "348MiB").
	GoMemLimit string
	// LimitMiB is the memory_limiter hard limit (limit_mib).
	LimitMiB int
	// SpikeMiB is the memory_limiter spike buffer (spike_limit_mib); the soft limit is LimitMiB - SpikeMiB.
	SpikeMiB int
	// SoftMiB is the memory_limiter soft limit (LimitMiB - SpikeMiB), the point at which the collector starts
	// refusing data. GOMEMLIMIT must stay below it.
	SoftMiB int
}

// DeriveCollectorMemorySettings derives the memory_limiter thresholds and GOMEMLIMIT for a collector from its
// container memory limit, enforcing GOMEMLIMIT < soft < hard < limit so the Go runtime paces GC before the
// memory_limiter forces GC and refuses telemetry. The margins are absolute floors with a percentage slope, because a
// collector's overhead on top of its live heap (stacks, off-heap file_storage, fragmentation) does not scale linearly
// with the limit; the effective ratios therefore grow with it (~80% hard limit at 500Mi, ~90% at >=1Gi).
// The second return value is false when no limit is set (or it is too small for the floors), in which case the caller
// keeps the percentage-based memory_limiter fallback and leaves GOMEMLIMIT unset.
func DeriveCollectorMemorySettings(limit resource.Quantity) (CollectorMemorySettings, bool) {
	l := int(limit.Value() / mibBytes)
	if l <= 0 {
		return CollectorMemorySettings{}, false
	}

	reserve := clampInt(l*10/100, 96, 512) // RSS reserve, hard -> limit
	spike := clampInt(l*5/100, 32, 256)    // spike buffer, soft -> hard
	goMemGap := clampInt(l*3/100, 24, 128) // gomemlimit -> soft

	hard := l - reserve
	soft := hard - spike
	goMem := soft - goMemGap
	if goMem <= 0 || soft <= 0 || hard <= 0 {
		// The limit is too small for the absolute floors; fall back to percentages.
		return CollectorMemorySettings{}, false
	}

	return CollectorMemorySettings{
		GoMemLimit: fmt.Sprintf("%dMiB", goMem),
		LimitMiB:   hard,
		SpikeMiB:   spike,
		SoftMiB:    soft,
	}, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// mainCollectorMemoryResources returns the collector specs whose GOMEMLIMIT and memory_limiter are auto-derived from
// the memory limit, keyed by a human-readable name for logging. The auxiliary containers (config reloader, filelog
// offset sync) are excluded: they run no memory_limiter and their limits are below the derivation floors.
func (ec *ExtraConfig) mainCollectorMemoryResources() map[string]*ResourceRequirementsWithGoMemLimit {
	return map[string]*ResourceRequirementsWithGoMemLimit{
		"daemonset collector":      &ec.CollectorDaemonSetCollectorContainerResources,
		"deployment collector":     &ec.CollectorDeploymentCollectorContainerResources,
		"signal-control collector": &ec.SignalControlCollectorContainerResources,
	}
}

// EffectiveGoMemLimit returns the GOMEMLIMIT for a collector container: the configured value if set, otherwise the
// value derived from the memory limit (see DeriveCollectorMemorySettings), or "" when neither is available.
func (rr ResourceRequirementsWithGoMemLimit) EffectiveGoMemLimit() string {
	if rr.GoMemLimit != "" {
		return rr.GoMemLimit
	}
	if settings, ok := DeriveCollectorMemorySettings(*rr.Limits.Memory()); ok {
		return settings.GoMemLimit
	}
	return ""
}

// DeriveGoMemLimitFromLimit derives the GOMEMLIMIT for a container that runs no memory_limiter from its memory limit:
// percent of the limit, but never leaving less than goMemLimitMinHeadroomMiB of headroom below it. The second return
// value is false when no memory limit is set or it is too small to leave any headroom, in which case GOMEMLIMIT is unset.
func DeriveGoMemLimitFromLimit(limit resource.Quantity, percent int) (string, bool) {
	l := int(limit.Value() / mibBytes)
	if l <= 0 {
		return "", false
	}
	goMem := min(l*percent/100, l-goMemLimitMinHeadroomMiB)
	if goMem <= 0 {
		return "", false
	}
	return fmt.Sprintf("%dMiB", goMem), true
}

// EffectiveGoMemLimitPercent returns the GOMEMLIMIT for a container that runs no memory_limiter: the configured value
// if set, otherwise the value derived from the memory limit at percent (see DeriveGoMemLimitFromLimit), or "" when
// neither is available.
func (rr ResourceRequirementsWithGoMemLimit) EffectiveGoMemLimitPercent(percent int) string {
	if rr.GoMemLimit != "" {
		return rr.GoMemLimit
	}
	if goMemLimit, ok := DeriveGoMemLimitFromLimit(*rr.Limits.Memory(), percent); ok {
		return goMemLimit
	}
	return ""
}

// goMemLimitInversion describes a main collector whose explicitly configured GOMEMLIMIT is not below the
// memory_limiter soft limit derived from its memory limit.
type goMemLimitInversion struct {
	Collector  string
	GoMemLimit string
	SoftMiB    int
}

// collectorGoMemLimitInversions returns one entry per main collector whose explicitly configured GOMEMLIMIT is at or
// above the derived memory_limiter soft limit (a misconfiguration; see DeriveCollectorMemorySettings). Collectors with
// no explicit GOMEMLIMIT, no derivable limit, or an unparseable GOMEMLIMIT are skipped, as are auto-derived values.
func (ec ExtraConfig) collectorGoMemLimitInversions() []goMemLimitInversion {
	var inversions []goMemLimitInversion
	for name, res := range ec.mainCollectorMemoryResources() {
		if res.GoMemLimit == "" {
			continue
		}
		settings, ok := DeriveCollectorMemorySettings(*res.Limits.Memory())
		if !ok {
			continue
		}
		goMemMiB, parsed := parseGoMemLimitMiB(res.GoMemLimit)
		if !parsed {
			continue
		}
		if goMemMiB >= settings.SoftMiB {
			inversions = append(inversions, goMemLimitInversion{
				Collector:  name,
				GoMemLimit: res.GoMemLimit,
				SoftMiB:    settings.SoftMiB,
			})
		}
	}
	return inversions
}

// WarnOnCollectorGoMemLimitInversion logs a warning for every main collector whose explicitly configured
// GOMEMLIMIT is not below the derived memory_limiter soft limit (see collectorGoMemLimitInversions).
func WarnOnCollectorGoMemLimitInversion(ec ExtraConfig, logger logd.Logger) {
	for _, inversion := range ec.collectorGoMemLimitInversions() {
		logger.Warn(
			"the configured GOMEMLIMIT is not below the memory_limiter soft limit; the collector may refuse "+
				"telemetry prematurely, set gomemlimit below the soft limit or leave it empty to auto-derive",
			"collector", inversion.Collector,
			"gomemlimit", inversion.GoMemLimit,
			"memoryLimiterSoftLimitMiB", inversion.SoftMiB,
		)
	}
}

// parseGoMemLimitMiB parses a Go GOMEMLIMIT string (a number with an optional B/KiB/MiB/GiB/TiB suffix, see
// https://pkg.go.dev/runtime#hdr-Environment_Variables) into MiB. It returns false when the value cannot be
// parsed, in which case callers skip the value rather than treating it as a misconfiguration.
func parseGoMemLimitMiB(v string) (int, bool) {
	v = strings.TrimSpace(v)
	suffixes := []struct {
		suffix string
		bytes  int64
	}{
		{"TiB", 1 << 40},
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
		{"B", 1},
	}
	for _, s := range suffixes {
		if strings.HasSuffix(v, s.suffix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(v, s.suffix)), 10, 64)
			if err != nil {
				return 0, false
			}
			return int(n * s.bytes / mibBytes), true
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return int(n / mibBytes), true
}
