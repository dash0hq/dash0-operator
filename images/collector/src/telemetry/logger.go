// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service/telemetry"
	"go.uber.org/zap"
)

// The paths are variables so that tests can replace them.
var (
	podLogsDir        = "/var/log/pods"
	mountsFile        = "/proc/self/mounts"
	securityLabelFile = "/proc/self/attr/current"
)

// createLoggerWithPodLogsCheck returns a CreateLoggerFunc that delegates to the wrapped factory's CreateLogger and then
// checks the pod log directory via the resulting logger (see warnIfNoPodLogFilesFound).
func createLoggerWithPodLogsCheck(base telemetry.Factory) telemetry.CreateLoggerFunc {
	return func(
		ctx context.Context,
		set telemetry.LoggerSettings,
		cfg component.Config,
	) (*zap.Logger, component.ShutdownFunc, error) {
		logger, shutdown, err := base.CreateLogger(ctx, set, cfg)
		if err != nil {
			return logger, shutdown, err
		}
		// The check runs in the background so that a slow file system cannot delay collector startup.
		go warnIfNoPodLogFilesFound(logger)
		return logger, shutdown, nil
	}
}

// warnIfNoPodLogFilesFound logs a warning when the pod log directory is mounted (only the DaemonSet collector mounts
// it) but cannot be listed or contains no log file. The filelog receiver logs a pod log file it cannot open, but a pod
// log directory it cannot list only at debug level, so log collection would silently find nothing.
func warnIfNoPodLogFilesFound(logger *zap.Logger) {
	if !isMountPoint(podLogsDir) {
		return
	}
	var listingErr error
	logFileFound := false
	_ = filepath.WalkDir(podLogsDir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Only the failing directory is skipped, a pod directory might just have been removed.
			if listingErr == nil {
				listingErr = err
			}
			return fs.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			logFileFound = true
			return fs.SkipAll
		}
		return nil
	})
	if logFileFound {
		return
	}
	groups, _ := os.Getgroups()
	logger.Warn(
		"No pod log files found, log collection (if enabled) will not find any logs.",
		zap.String("directory", podLogsDir),
		zap.Error(listingErr),
		zap.Int("uid", os.Getuid()),
		zap.Int("gid", os.Getgid()),
		zap.Ints("groups", groups),
		zap.String("security_label", readSecurityLabel()),
	)
}

func isMountPoint(dir string) bool {
	mounts, err := os.ReadFile(mountsFile)
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(mounts)) {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == dir {
			return true
		}
	}
	return false
}

// readSecurityLabel returns the security context of the collector process (e.g. its SELinux context), or an empty
// string when no security module provides one.
func readSecurityLabel() string {
	label, _ := os.ReadFile(securityLabelFile)
	return strings.TrimRight(string(label), "\x00\n")
}
