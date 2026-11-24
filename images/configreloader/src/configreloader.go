// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
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

	podIpEnvVarName                    = "DASH0_POD_IP"
	collectorHealthCheckPortEnvVarName = "DASH0_COLLECTOR_HEALTH_CHECK_PORT"
	collectorHealthCheckTimeout        = time.Second
	deferredReloadRetryInterval        = time.Second

	meterName       = "dash0.operator.configuration_reloader"
	metricNameLabel = "metric.name"
	errorLabel      = "error"
)

var (
	logger *slog.Logger

	configurationFileHashes = make(map[string]string)

	collectorHealthCheckClient = &http.Client{
		Timeout: collectorHealthCheckTimeout,
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
		},
	}
	errCollectorNotReady = errors.New("the collector is not ready")
	signalCollector      = triggerConfigurationReload

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

	common.StartPprofServerIfConfigured(logger)

	collectorPidFilePath := flag.String(
		"pidfile",
		"",
		"path to the collector's pid file, which contains the PID of the collector's process",
	)
	decompressedOutputPath := flag.String(
		"decompressedoutput",
		"",
		"path where the decompressed configuration file should be written after a change is detected (in case config map"+
			" compression is used)",
	)

	// Note: The checkFrequency of the configreloader is not the determining factor in how fast a changed config map will
	// be noticed and a config update in the collector process will be triggered. Instead, this is mostly determined by
	// the kubelet sync loop - the kubelet periodically polls the API server to detect changes to ConfigMaps/Secrets.
	// This interval is controlled by the kubelet's syncFrequency setting, see
	//nolint:lll
	// https://kubernetes.io/docs/reference/config-api/kubelet-config.v1beta1/#kubelet-config-k8s-io-v1beta1-KubeletConfiguration
	// This defaults to 1 minute. So it might take up to a minute from changing the config map content until the
	// configreloader can see the config change.
	checkFrequency := flag.Duration(
		"frequency",
		5*time.Second,
		"how often to check for changes in the configuration files",
	)

	flag.Parse()

	// The list of paths to watch for changes, if --decompressedoutput is set, this is assumed to be gzip compressed files
	// that will be unzipped to the --decompressedoutput directory; if --decompressedoutput is not set, the files are
	// assumed to be uncompressed.
	configurationFilePaths := flag.Args()

	if len(configurationFilePaths) < 1 {
		logger.Error("No configuration file paths provided in input")
		os.Exit(1)
	}

	if *decompressedOutputPath != "" {
		logger.Info("--decompressedoutput is set", "value", decompressedOutputPath)
	}

	if err := initializeHashes(configurationFilePaths); err != nil {
		logger.Error(
			"Cannot initialize hashes of configuration files",
			errorLabel,
			err,
		)
		os.Exit(1)
	}

	collectorHealthCheckUrl := collectorHealthCheckUrlFromEnv()
	if collectorHealthCheckUrl == "" {
		logger.Info(fmt.Sprintf(
			"%s or %s is not set, configuration updates will not wait for the collector to be ready",
			podIpEnvVarName,
			collectorHealthCheckPortEnvVarName,
		))
	}

	ticker := time.NewTicker(*checkFrequency)
	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown, syscall.SIGTERM)

	meter := common.InitOTelSdkFromEnvVars(ctx, meterName, "configuration-reloader")
	initializeSelfMonitoringMetrics(meter)

	go func() {
		var retryDeferredReload <-chan time.Time
		for {
			select {
			case <-ticker.C:
			case <-retryDeferredReload:
			case <-shutdown:
				ticker.Stop()
				done <- true
				return
			}

			isUpdateTriggered, err := checkConfiguration(
				ctx,
				configurationFilePaths,
				*decompressedOutputPath,
				*collectorPidFilePath,
				collectorHealthCheckUrl,
			)
			if errors.Is(err, errCollectorNotReady) {
				if retryDeferredReload == nil {
					logger.Info(
						"Deferring the collector configuration update until the collector is ready",
						errorLabel,
						err,
					)
				}
				retryDeferredReload = time.After(deferredReloadRetryInterval)
				continue
			}
			retryDeferredReload = nil
			if err != nil {
				logger.Info("An error occurred while checking for configuration changes", errorLabel, err)
			} else if isUpdateTriggered {
				logger.Info("Triggered collector configuration update")
			}
		}
	}()

	<-done

	common.ShutDownOTelSdk(ctx)
}

func collectorHealthCheckUrlFromEnv() string {
	podIp := os.Getenv(podIpEnvVarName)
	port := os.Getenv(collectorHealthCheckPortEnvVarName)
	if podIp == "" || port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(podIp, port) + "/"
}

func initializeHashes(configurationFilePaths []string) error {
	for _, configurationFilePath := range configurationFilePaths {
		hashValue, err := hashConfigurationFile(configurationFilePath)
		if err != nil {
			return err
		}
		configurationFileHashes[configurationFilePath] = hashValue
	}

	return nil
}

func hashConfigurationFile(configurationFilePath string) (string, error) {
	configurationFile, err := os.Open(configurationFilePath)
	if err != nil {
		return "", fmt.Errorf("cannot open configuration file '%s': %w", configurationFilePath, err)
	}
	defer func() {
		_ = configurationFile.Close()
	}()

	configurationFileHash := md5.New()
	if _, err := io.Copy(configurationFileHash, configurationFile); err != nil {
		return "", fmt.Errorf("cannot hash configuration file '%s': %w", configurationFilePath, err)
	}
	return hex.EncodeToString(configurationFileHash.Sum(nil)), nil
}

type HasTriggeredReload bool

/*
checkConfiguration compares the configuration files with the hashes of the configuration that has last been applied, and
sends SIGHUP to the collector if they differ. The collector only installs its SIGHUP handler after all its components
have been started; a SIGHUP that arrives earlier terminates the process. Hence the reload is deferred (by returning an
error wrapping errCollectorNotReady) until the collector's health check reports it as ready. The new hashes are only
recorded after the collector has been signalled successfully, so that a reload that has been deferred or that failed is
attempted again on the next check.
*/
func checkConfiguration(
	ctx context.Context,
	configurationFilePaths []string,
	decompressedOutputPath string,
	collectorPidFilePath string,
	collectorHealthCheckUrl string,
) (HasTriggeredReload, error) {
	var updatesConfigurationFilePaths []string
	updatedConfigurationFileHashes := make(map[string]string)
	// We need to poll files, the filesystem timestamps are not reliable in container runtime
	for _, configurationFilePath := range configurationFilePaths {
		hashValue, err := hashConfigurationFile(configurationFilePath)
		if err != nil {
			return false, err
		}

		if previousConfigurationFileHash, ok := configurationFileHashes[configurationFilePath]; ok {
			if previousConfigurationFileHash != hashValue {
				updatesConfigurationFilePaths = append(updatesConfigurationFilePaths, configurationFilePath)
				updatedConfigurationFileHashes[configurationFilePath] = hashValue
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

	if err := checkCollectorReadiness(ctx, collectorHealthCheckUrl); err != nil {
		return false, fmt.Errorf("%w: %w", errCollectorNotReady, err)
	}

	changedFiles := strings.Join(updatesConfigurationFilePaths, ",")
	if decompressedOutputPath != "" {
		if err := decompressAllConfigFiles(ctx, updatesConfigurationFilePaths, decompressedOutputPath); err != nil {
			return false, err
		}
	}

	logger.Info("Triggering a collector update due to changes to the config files", "changedFiles", changedFiles)

	collectorPid, err := parsePidFile(collectorPidFilePath)
	if err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotParseCollectorPid"),
			attribute.String("error.message", common.TruncateErrorForMetricAttribute(err)),
		))
		return false, fmt.Errorf("cannot retrieve collector pid: %w", err)
	}

	if err = signalCollector(collectorPid); err != nil {
		reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotTriggerCollectorUpdate"),
			attribute.String("error.message", common.TruncateErrorForMetricAttribute(err)),
		))
		return false, fmt.Errorf("cannot trigger collector update: %w", err)
	}

	maps.Copy(configurationFileHashes, updatedConfigurationFileHashes)
	configFilesChangesMetric.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.Int(metricNamePrefix+"changed_files.count", len(updatesConfigurationFilePaths)),
		attribute.String(metricNamePrefix+"changed_files.names", changedFiles),
	))
	return true, nil
}

func checkCollectorReadiness(ctx context.Context, collectorHealthCheckUrl string) error {
	if collectorHealthCheckUrl == "" {
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, collectorHealthCheckUrl, nil)
	if err != nil {
		return err
	}
	response, err := collectorHealthCheckClient.Do(request)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the health check at %s responded with HTTP %d", collectorHealthCheckUrl, response.StatusCode)
	}
	return nil
}

func decompressAllConfigFiles(
	ctx context.Context,
	updatesConfigurationFilePaths []string,
	decompressedOutputPath string,
) error {
	for _, updatedPath := range updatesConfigurationFilePaths {
		if err := decompressOneConfigFile(updatedPath, decompressedOutputPath); err != nil {
			reloadErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("error.type", "CannotDecompressConfig"),
				attribute.String("error.message", common.TruncateErrorForMetricAttribute(err)),
			))
			return fmt.Errorf("cannot decompress updated config file: %w", err)
		}
	}
	return nil
}

func decompressOneConfigFile(compressedPath string, decompressedOutputPath string) error {
	logger.Info("Unpacking file", "from", compressedPath, "to", decompressedOutputPath)
	compressedFile, err := os.Open(compressedPath)
	if err != nil {
		return fmt.Errorf("cannot open compressed config file '%s': %w", compressedPath, err)
	}
	defer func() {
		_ = compressedFile.Close()
	}()

	gzReader, err := gzip.NewReader(compressedFile)
	if err != nil {
		return fmt.Errorf("cannot create gzip reader for '%s': %w", compressedPath, err)
	}
	defer func() {
		_ = gzReader.Close()
	}()

	outputFile, err := os.Create(decompressedOutputPath)
	if err != nil {
		return fmt.Errorf("cannot create decompressed output file '%s': %w", decompressedOutputPath, err)
	}
	defer func() {
		_ = outputFile.Close()
	}()

	if _, err = io.Copy(outputFile, gzReader); err != nil {
		return fmt.Errorf("cannot decompress config file '%s' to '%s': %w", compressedPath, decompressedOutputPath, err)
	}

	logger.Info("Unpacked file successfully", "from", compressedPath, "to", decompressedOutputPath)
	return nil
}

func parsePidFile(pidFilePath string) (int, error) {
	collectorPidFile, err := os.Open(pidFilePath)
	if err != nil {
		return 0, fmt.Errorf("cannot open '%s' pid file: %w", pidFilePath, err)
	}
	defer func() {
		_ = collectorPidFile.Close()
	}()

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
			err,
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
			err,
		)
		os.Exit(1)
	}
}
