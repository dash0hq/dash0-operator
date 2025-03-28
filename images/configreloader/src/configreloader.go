// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	slogmulti "github.com/samber/slog-multi"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

const (
	// Technically, one can change the max pid via the /proc/sys/kernel/pid_max interface.
	// But I have never seen this done, and a container that runs through 32_768 PIDs is
	// doing something really nonsensical.
	PID_MAX_LIMIT = 32_768

	meterName       = "dash0.operator.configuration_reloader"
	metricNameLabel = "metric.name"
	errorLabel      = "error"
)

var (
	logger *slog.Logger

	configurationFileHashes = make(map[string]string)

	metricNamePrefix = fmt.Sprintf("%s.", meterName)

	configFilesChangesMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "config_file_changes")
	configFilesChangesMetric     otelmetric.Int64Counter

	reloadErrorsMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "reload_errors")
	reloadErrorsMetric     otelmetric.Int64Counter
)

func main() {
	ctx := context.Background()

	stdOutSlogHandler := slog.NewJSONHandler(os.Stdout, nil)
	if common.OTelSDKIsConfigured() {
		logger = slog.New(
			slogmulti.Fanout(
				stdOutSlogHandler,
				otelslog.NewHandler("configurationreloader"),
			),
		)
	} else {
		logger = slog.New(stdOutSlogHandler)
	}

	collectorPidFilePath := flag.String(
		"pidfile",
		"",
		"path to the collector's pid file, which contains the PID of the collector's process",
	)
	checkFrequency := flag.Duration(
		"frequency",
		1*time.Second,
		"how often to check for changes in the configuration files",
	)

	flag.Parse()

	configurationFilePaths := flag.Args()

	if len(configurationFilePaths) < 1 {
		logger.Error("No configuration file paths provided in input")
		os.Exit(1)
	}

	if err := initializeHashes(configurationFilePaths); err != nil {
		logger.Error(
			"Cannot initialize hashes of configuration files",
			errorLabel,
			common.TruncateErrorForLogAttribute(err),
		)
		os.Exit(1)
	}

	ticker := time.NewTicker(*checkFrequency)
	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown, syscall.SIGTERM)

	meter := common.InitOTelSdkFromEnvVars(ctx, meterName, "dash0-operator-collector", "configuration-reloader")
	initializeSelfMonitoringMetrics(meter)

	go func() {
		for {
			select {
			case <-ticker.C:
				if isUpdateTriggered, err := checkConfiguration(
					ctx,
					configurationFilePaths,
					*collectorPidFilePath,
				); err != nil {
					logger.Info(
						"An error occurred while check for configuration changes",
						errorLabel,
						common.TruncateErrorForLogAttribute(err),
					)
				} else if isUpdateTriggered {
					logger.Info("Triggered collector configuration update")
				}
			case <-shutdown:
				ticker.Stop()
				done <- true
			}
		}
	}()

	<-done

	common.ShutDownOTelSdk(ctx)
}

func initializeHashes(configurationFilePaths []string) error {
	for _, configurationFilePath := range configurationFilePaths {
		configurationFile, err := os.Open(configurationFilePath)
		if err != nil {
			return fmt.Errorf("cannot open configuration file '%s': %w", configurationFilePath, err)
		}
		defer configurationFile.Close()

		configurationFileHash := md5.New()
		if _, err := io.Copy(configurationFileHash, configurationFile); err != nil {
			return fmt.Errorf("cannot hash configuration file '%s': %w", configurationFilePath, err)
		}
		hashValue := hex.EncodeToString(configurationFileHash.Sum(nil))
		configurationFileHashes[configurationFilePath] = hashValue
	}

	return nil
}

type HasTriggeredReload bool

func checkConfiguration(
	ctx context.Context,
	configurationFilePaths []string,
	collectorPidFilePath string,
) (HasTriggeredReload, error) {
	var updatesConfigurationFilePaths []string
	// We need to poll files, the filesystem timestamps are not reliable in container runtime
	for _, configurationFilePath := range configurationFilePaths {
		configurationFile, err := os.Open(configurationFilePath)
		if err != nil {
			return false, fmt.Errorf("cannot open configuration file '%s': %w", configurationFilePath, err)
		}
		defer configurationFile.Close()

		configurationFileHash := md5.New()
		if _, err := io.Copy(configurationFileHash, configurationFile); err != nil {
			return false, fmt.Errorf("cannot hash configuration file '%s': %w", configurationFilePath, err)
		}
		hashValue := hex.EncodeToString(configurationFileHash.Sum(nil))

		if previousConfigurationFileHash, ok := configurationFileHashes[configurationFilePath]; ok {
			if previousConfigurationFileHash != hashValue {
				logger.Info("configuration file has been updated", "configurationFilePath", configurationFilePath)
				updatesConfigurationFilePaths = append(updatesConfigurationFilePaths, configurationFilePath)
				configurationFileHashes[configurationFilePath] = hashValue
			}
		} else {
			// This is the first time we hash this config file, nothing to do beyond updating. This should actually
			// never happen since we initialize the hashes for all config files at startup in initializeHashes and
			// config files cannot be added retroactively to a running collector pod.
			configurationFileHashes[configurationFilePath] = hashValue
			logger.Info("initialized hash for configuration file", "configurationFilePath", configurationFilePath)
		}
	}

	if len(updatesConfigurationFilePaths) < 1 {
		// nothing to do
		return false, nil
	}

	changedFiles := strings.Join(updatesConfigurationFilePaths, ",")
	configFilesChangesMetric.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.Int(metricNamePrefix+"changed_files.count", len(updatesConfigurationFilePaths)),
		attribute.String(metricNamePrefix+"changed_files.names", changedFiles),
	))

	logger.Info("Triggering a collector update due to changes to the config files", "changedFiles", changedFiles)

	collectorPid, err := parsePidFile(collectorPidFilePath)
	if err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotParseCollectorPid"),
			attribute.String("error.message", common.TruncateErrorForMetricAttribute(err)),
		))
		return false, fmt.Errorf("cannot retrieve collector pid: %w", err)
	}

	if err = triggerConfigurationReload(collectorPid); err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotTriggerCollectorUpdate"),
			attribute.String("error.message", common.TruncateErrorForMetricAttribute(err)),
		))
		return false, fmt.Errorf("cannot trigger collector update: %w", err)
	}

	return true, nil
}

func parsePidFile(pidFilePath string) (int, error) {
	collectorPidFile, err := os.Open(pidFilePath)
	if err != nil {
		return 0, fmt.Errorf("cannot open '%s' pid file: %w", pidFilePath, err)
	}
	defer collectorPidFile.Close()

	scanner := bufio.NewScanner(collectorPidFile)
	scanner.Split(bufio.ScanWords)

	if !scanner.Scan() {
		return 0, fmt.Errorf("pid file '%s' is empty", pidFilePath)
	}

	firstWord := scanner.Text()
	pid, err := strconv.Atoi(firstWord)
	if err != nil {
		return 0, fmt.Errorf(
			"pid file '%s' has an unexpected format: expecting a single integer; found: %s",
			pidFilePath,
			firstWord,
		)
	}

	if scanner.Scan() {
		// TODO Get all the rest of the file
		return 0, fmt.Errorf(
			"pid file '%s' has an unexpected format: expecting a single integer; found additional content: %s",
			pidFilePath,
			scanner.Text(),
		)
	}

	logger.Info("parsed collector pid", "pid", pid)
	return pid, nil
}

func triggerConfigurationReload(collectorPid int) error {
	if collectorPid < 0 || collectorPid > PID_MAX_LIMIT {
		return fmt.Errorf(
			"unexpected pid: expecting an integer between 0 and %d, found additional: %d",
			PID_MAX_LIMIT,
			collectorPid,
		)
	}

	collectorProcess, err := os.FindProcess(collectorPid)
	if err != nil {
		return fmt.Errorf("cannot find the process with pid '%d': %w", collectorPid, err)
	}

	logger.Info("sending SIGHUP signal to the process with pid", "pid", collectorPid)
	if err := collectorProcess.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf(
			"an error occurred while sending the SIGHUP signal to the process with pid '%d': %w",
			collectorPid,
			err,
		)
	}

	return nil
}

func initializeSelfMonitoringMetrics(meter otelmetric.Meter) {
	var err error

	if configFilesChangesMetric, err = meter.Int64Counter(
		configFilesChangesMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for configuration file change events that trigger a reload"),
	); err != nil {
		logger.Error(
			"cannot initialize the metric",
			metricNameLabel,
			configFilesChangesMetricName,
			errorLabel,
			common.TruncateErrorForLogAttribute(err),
		)
		os.Exit(1)
	}

	if reloadErrorsMetric, err = meter.Int64Counter(
		reloadErrorsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Error counter for failed reload attempts"),
	); err != nil {
		logger.Error(
			"cannot initialize the metric",
			metricNameLabel,
			reloadErrorsMetricName,
			errorLabel,
			common.TruncateErrorForLogAttribute(err),
		)
		os.Exit(1)
	}
}
