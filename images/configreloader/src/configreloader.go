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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
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
	meterProvider    otelmetric.MeterProvider

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

	meter, selfMonitoringShutdownFunctions := setUpSelfMonitoring(ctx)
	setUpSelfMonitoringMetrics(meter)

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

	shutDownSelfMonitoring(ctx, selfMonitoringShutdownFunctions)
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

func setUpSelfMonitoring(ctx context.Context) (otelmetric.Meter, []func(ctx context.Context) error) {
	podUid, isSet := os.LookupEnv("K8S_POD_UID")
	if !isSet {
		log.Println("Env var 'K8S_POD_UID' is not set")
	}

	nodeName, isSet := os.LookupEnv("K8S_NODE_NAME")
	if !isSet {
		log.Println("Env var 'K8S_NODE_NAME' is not set")
	}

	var doMeterShutdown func(ctx context.Context) error

	if _, isSet = os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); isSet {
		var metricExporter sdkmetric.Exporter

		protocol, isProtocolSet := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
		if !isProtocolSet {
			// http/protobuf is the default transport protocol, see spec:
			// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
			protocol = "http/protobuf"
		}

		var err error
		switch protocol {
		case "grpc":
			if metricExporter, err = otlpmetricgrpc.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP gRPC metrics exporter: %v", err)
			}
		case "http/protobuf":
			if metricExporter, err = otlpmetrichttp.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP HTTP metrics exporter: %v", err)
			}
		case "http/json":
			log.Fatalf("Cannot create the OTLP HTTP exporter: the protocol 'http/json' is currently unsupported")
		default:
			log.Fatalf("Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v", protocol)
		}

		r, err := resource.New(ctx,
			resource.WithAttributes(
				semconv.K8SPodUID(podUid),
				semconv.K8SNodeName(nodeName),
			),
		)
		if err != nil {
			log.Fatalf("Cannot setup the OTLP resource: %v", err)
		}

		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(r),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(5*time.Second),
				)),
		)

		meterProvider = sdkMeterProvider
		doMeterShutdown = sdkMeterProvider.Shutdown
	} else {
		meterProvider = metricnoop.MeterProvider{}
		doMeterShutdown = func(ctx context.Context) error { return nil }
	}

	otel.SetMeterProvider(meterProvider)

	return meterProvider.Meter(meterName), []func(ctx context.Context) error{
		doMeterShutdown,
	}
}

func shutDownSelfMonitoring(ctx context.Context, shutdownFunctions []func(ctx context.Context) error) {
	var err error
	for _, shutdownFunction := range shutdownFunctions {
		timeoutCtx, cancelFun := context.WithTimeout(ctx, time.Second)
		if err = shutdownFunction(timeoutCtx); err != nil {
			log.Printf("Failed to shutdown self monitoring, telemetry may have been lost:%v\n", err)
		}
		cancelFun()
	}
}

func setUpSelfMonitoringMetrics(meter otelmetric.Meter) {
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
