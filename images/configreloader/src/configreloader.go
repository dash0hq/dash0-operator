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
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

const (
	// Technically, one can change the max pid via the /proc/sys/kernel/pid_max interface.
	// But I have never seen this done, and a container that runs through 32_768 PIDs is
	// doing something really nonsensical.
	PID_MAX_LIMIT = 32_768

	meterName = "dash0.operator.configuration_reloader"
)

var (
	configurationFileHashes = make(map[string]string)

	metricNamePrefix = fmt.Sprintf("%s.", meterName)

	configFilesChangesMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "config_file_changes")
	configFilesChangesMetric     otelmetric.Int64Counter

	reloadErrorsMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "reload_errors")
	reloadErrorsMetric     otelmetric.Int64Counter
)

func main() {
	ctx := context.Background()

	collectorPidFilePath := flag.String("pidfile", "", "path to the collector's pid file, which contains the PID of the collector's process")
	checkFrequency := flag.Duration("frequency", 1*time.Second, "how often to check for changes in the configuration files")

	flag.Parse()

	configurationFilePaths := flag.Args()

	if len(configurationFilePaths) < 1 {
		log.Fatalf("No configuration file paths provided in input")
	}

	log.SetOutput(os.Stdout)

	if err := initializeHashes(configurationFilePaths); err != nil {
		log.Fatalf("Cannot initialize hashes of configuration files: %v", err)
	}

	ticker := time.NewTicker(*checkFrequency)
	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown, syscall.SIGTERM)

	meter, selfMonitoringShutdownFunctions := common.InitOTelSdk(ctx, meterName, nil)
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
					log.Printf("An error occurred while check for configuration changes: %s\n", err)
				} else if isUpdateTriggered {
					log.Println("Triggered collector configuration update")
				}
			case <-shutdown:
				ticker.Stop()
				done <- true
			}
		}
	}()

	<-done

	common.ShutDownOTelSdk(ctx, selfMonitoringShutdownFunctions)
}

func initializeHashes(configurationFilePaths []string) error {
	for _, configurationFilePath := range configurationFilePaths {
		configurationFile, err := os.Open(configurationFilePath)
		if err != nil {
			return fmt.Errorf("cannot open '%v' configuration file: %w", configurationFilePath, err)
		}
		defer configurationFile.Close()

		configurationFileHash := md5.New()
		if _, err := io.Copy(configurationFileHash, configurationFile); err != nil {
			return fmt.Errorf("cannot hash '%v' configuration file: %w", configurationFilePath, err)
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
			return false, fmt.Errorf("cannot open '%v' configuration file: %w", configurationFilePath, err)
		}
		defer configurationFile.Close()

		configurationFileHash := md5.New()
		if _, err := io.Copy(configurationFileHash, configurationFile); err != nil {
			return false, fmt.Errorf("cannot hash '%v' configuration file: %w", configurationFilePath, err)
		}

		hashValue := hex.EncodeToString(configurationFileHash.Sum(nil))
		if previousConfigurationFileHash, ok := configurationFileHashes[configurationFilePath]; ok {
			if previousConfigurationFileHash != hashValue {
				updatesConfigurationFilePaths = append(updatesConfigurationFilePaths, configurationFilePath)
				// Update hash; we cannot check whether the collector *actually* updated,
				// so we optimistically update the internal state anyhow
				configurationFileHashes[configurationFilePath] = hashValue
			}
		} else {
			// First time we hash the config file, nothing to do beyond updating
			configurationFileHashes[configurationFilePath] = hashValue
		}
	}

	if len(updatesConfigurationFilePaths) < 1 {
		// Nothing to do
		return false, nil
	}

	changedFiles := strings.Join(updatesConfigurationFilePaths, ",")
	configFilesChangesMetric.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.Int(metricNamePrefix+"changed_files.count", len(updatesConfigurationFilePaths)),
		attribute.String(metricNamePrefix+"changed_files.names", changedFiles),
	))

	log.Printf("Triggering a collector update due to changes to the config files: %v\n", changedFiles)

	collectorPid, err := parsePidFile(collectorPidFilePath)
	if err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotParseCollectorPid"),
			attribute.String("error.message", err.Error()),
		))
		return false, fmt.Errorf("cannot retrieve collector pid: %w", err)
	}

	if err := triggerConfigurationReload(collectorPid); err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotTriggerCollectorUpdate"),
			attribute.String("error.message", err.Error()),
		))
		return false, fmt.Errorf("cannot trigger collector update: %w", err)
	}

	return true, nil
}

type OTelColPid int

func parsePidFile(pidFilePath string) (OTelColPid, error) {
	collectorPidFile, err := os.Open(pidFilePath)
	if err != nil {
		return 0, fmt.Errorf("cannot open '%v' pid file: %w", pidFilePath, err)
	}
	defer collectorPidFile.Close()

	scanner := bufio.NewScanner(collectorPidFile)
	scanner.Split(bufio.ScanWords)

	if !scanner.Scan() {
		return 0, fmt.Errorf("pid file '%v' is empty", pidFilePath)
	}

	firstWord := scanner.Text()
	pid, err := strconv.Atoi(firstWord)
	if err != nil {
		return 0, fmt.Errorf("pid file '%v' has an unexpected format: expecting a single integer; found: %v", pidFilePath, firstWord)
	}

	if scanner.Scan() {
		// TODO Get all the rest of the file
		return 0, fmt.Errorf("pid file '%v' has an unexpected format: expecting a single integer; found additional content: %v", pidFilePath, scanner.Text())
	}

	return OTelColPid(pid), nil
}

func triggerConfigurationReload(collectorPid OTelColPid) error {
	if collectorPid < 0 || collectorPid > PID_MAX_LIMIT {
		return fmt.Errorf("unexpected pid: expecting an integer between 0 and %v, found additional: %v", PID_MAX_LIMIT, collectorPid)
	}

	collectorProcess, err := os.FindProcess(int(collectorPid))
	if err != nil {
		return fmt.Errorf("cannot find the process with pid '%v': %w", collectorPid, err)
	}

	if err := collectorProcess.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("an error occurred while sending the SIGHUP signal to the process with pid '%v': %w", collectorPid, err)
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
		log.Fatalf("Cannot initialize the metric %s: %v", configFilesChangesMetricName, err)
	}

	if reloadErrorsMetric, err = meter.Int64Counter(
		reloadErrorsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Error counter for failed reload attempts"),
	); err != nil {
		log.Fatalf("Cannot initialize the metric %s: %v", reloadErrorsMetricName, err)
	}
}
