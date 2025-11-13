// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path"
)

const (
	metricsFilename = "metrics.jsonl"
	logsFilename    = "logs.jsonl"
	tracesFilename  = "traces.jsonl"
)

type Configuration struct {
	Port        string
	MetricsFile string
	LogsFile    string
	TracesFile  string
}

func newConfigurationFromEnvironment() Configuration {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8002"
	}

	telemetryDir := "/telemetry"
	if dir := os.Getenv("TELEMETRY_DIR"); dir != "" {
		telemetryDir = dir
	}

	return Configuration{
		Port:        port,
		MetricsFile: path.Join(telemetryDir, metricsFilename),
		LogsFile:    path.Join(telemetryDir, logsFilename),
		TracesFile:  path.Join(telemetryDir, tracesFilename),
	}
}
