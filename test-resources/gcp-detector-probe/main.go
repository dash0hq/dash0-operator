// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Command gcp-detector-probe replicates, step by step, what the gcp resource detector of the OpenTelemetry collector
// does on startup, and logs the start and the end of every single step.
//
// The gcp detector of the resourcedetection processor first calls CloudPlatform() of
// github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp and only then metadata.OnGCE().
// CloudPlatform() tries the platforms in a fixed order, and two of the checks ask the cloud instance metadata server
// rather than only reading environment variables:
//
//   - onGKE() reads KUBERNETES_SERVICE_HOST, which is always set inside a pod, and then calls
//     InstanceAttributeValueWithContext(context.TODO(), "cluster-location").
//   - onGCE() calls GetWithContext(context.TODO(), "instance/machine-type").
//
// Both pass context.TODO(), which carries no deadline, so the timeout that the resourcedetection processor configures
// cannot stop them. The metadata client dials with a timeout of 2s, caps a single request at 5s and retries up to 5
// times, which bounds one such call at roughly half a minute when the metadata endpoint drops the packets.
//
// This probe runs the same calls in the same order, so the log shows which step blocks and for how long. It is a
// diagnostic tool, it always exits with 0.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"cloud.google.com/go/compute/metadata"
	gcpdetector "github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp"
)

const (
	metadataHost = "metadata.google.internal"
	metadataIP   = "169.254.169.254"

	clusterLocationAttr = "cluster-location"
	machineTypeAttr     = "instance/machine-type"

	bmsProjectIDEnv        = "BMS_PROJECT_ID"
	bmsRegionEnv           = "BMS_REGION"
	bmsInstanceIDEnv       = "BMS_INSTANCE_ID"
	k8sServiceHostEnv      = "KUBERNETES_SERVICE_HOST"
	cloudFunctionTargetEnv = "FUNCTION_TARGET"
	cloudRunConfigEnv      = "K_CONFIGURATION"
	cloudRunJobEnv         = "CLOUD_RUN_JOB"
	cloudRunWorkerPoolEnv  = "CLOUD_RUN_WORKER_POOL"
	gaeEnv                 = "GAE_ENV"
	gaeServiceEnv          = "GAE_SERVICE"

	tcpDialTimeout = 10 * time.Second
)

var (
	started         = time.Now()
	errEnvVarNotSet = errors.New("not set")
)

func logf(format string, arguments ...any) {
	fmt.Printf("[t+%10s] %s\n", time.Since(started).Round(time.Millisecond), fmt.Sprintf(format, arguments...))
}

// runStep executes one step of the detector and logs both when it starts and when it returns. While the step runs it
// reports progress every two seconds, so that a step which blocks is visible in the log before it finishes, and a step
// which never returns at all is still attributable.
func runStep(name, call string, step func() (string, error)) {
	stepStarted := time.Now()
	logf("START  %-24s %s", name, call)

	finished := make(chan struct{})
	go reportProgress(name, stepStarted, finished)

	value, err := step()
	close(finished)

	elapsed := time.Since(stepStarted).Round(time.Millisecond)
	if err != nil {
		logf("DONE   %-24s elapsed=%-12s err=%v", name, elapsed, err)
		return
	}
	logf("DONE   %-24s elapsed=%-12s value=%q", name, elapsed, value)
}

func reportProgress(name string, stepStarted time.Time, finished <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-finished:
			return
		case <-ticker.C:
			logf("  ...  %-24s still running after %s", name, time.Since(stepStarted).Round(time.Millisecond))
		}
	}
}

func envStep(name, key string) {
	runStep(name, fmt.Sprintf("os.LookupEnv(%q)", key), func() (string, error) {
		value, found := os.LookupEnv(key)
		if !found {
			return "", errEnvVarNotSet
		}
		return value, nil
	})
}

// onBareMetalSolution only reads environment variables, it never reaches the network.
func probeBareMetalSolution() {
	envStep("onBareMetalSolution", bmsProjectIDEnv)
	envStep("onBareMetalSolution", bmsRegionEnv)
	envStep("onBareMetalSolution", bmsInstanceIDEnv)
}

// onGKE is the first check that asks the metadata server, and inside a pod it always gets that far, because
// KUBERNETES_SERVICE_HOST is set. The metadata call below is the prime suspect.
func probeGKE() {
	envStep("onGKE/env", k8sServiceHostEnv)
	call := fmt.Sprintf("metadata.InstanceAttributeValueWithContext(context.TODO(), %q)", clusterLocationAttr)
	runStep("onGKE/metadata", call, func() (string, error) {
		return metadata.InstanceAttributeValueWithContext(context.TODO(), clusterLocationAttr)
	})
}

// The Cloud Functions, Cloud Run and App Engine checks only read environment variables.
func probeFaaSAndAppEngine() {
	envStep("onCloudFunctions", cloudFunctionTargetEnv)
	envStep("onCloudRun", cloudRunConfigEnv)
	envStep("onCloudRunJob", cloudRunJobEnv)
	envStep("onCloudRunWorkerPool", cloudRunWorkerPoolEnv)
	envStep("onAppEngineStandard", gaeEnv)
	envStep("onAppEngine", gaeServiceEnv)
}

// onGCE is the last check of CloudPlatform() and asks the metadata server again.
func probeGCE() {
	call := fmt.Sprintf("metadata.GetWithContext(context.TODO(), %q)", machineTypeAttr)
	runStep("onGCE/metadata", call, func() (string, error) {
		return metadata.GetWithContext(context.TODO(), machineTypeAttr)
	})
}

// metadata.OnGCE is what the gcp detector of the resourcedetection processor calls once CloudPlatform() has returned.
// It takes no context at all, so it cannot honour a deadline either.
func probeOnGCE() {
	runStep("metadata.OnGCE", "metadata.OnGCE()", func() (string, error) {
		return fmt.Sprintf("%t", metadata.OnGCE()), nil
	})
}

// Reference measurements that tell a blocked metadata endpoint apart from a broken name resolution.
func probeReference() {
	address := net.JoinHostPort(metadataIP, "80")
	runStep("tcp-dial", fmt.Sprintf("net.DialTimeout(\"tcp\", %q, %s)", address, tcpDialTimeout), func() (string, error) {
		connection, err := net.DialTimeout("tcp", address, tcpDialTimeout)
		if err != nil {
			return "", err
		}
		defer func() { _ = connection.Close() }()
		return "connected", nil
	})

	runStep("dns-lookup", fmt.Sprintf("net.LookupHost(%q)", metadataHost), func() (string, error) {
		addresses, err := net.LookupHost(metadataHost)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%v", addresses), nil
	})
}

// The real detector, so that the total can be compared against the sum of the isolated steps above.
func probeRealDetector() {
	runStep("CloudPlatform", "gcp.NewDetector().CloudPlatform()", func() (string, error) {
		return platformName(gcpdetector.NewDetector().CloudPlatform()), nil
	})
}

func platformName(platform gcpdetector.Platform) string {
	switch platform {
	case gcpdetector.GKE:
		return "GKE"
	case gcpdetector.GCE:
		return "GCE"
	case gcpdetector.CloudRun:
		return "CloudRun"
	case gcpdetector.CloudRunJob:
		return "CloudRunJob"
	case gcpdetector.CloudRunWorkerPool:
		return "CloudRunWorkerPool"
	case gcpdetector.CloudFunctions:
		return "CloudFunctions"
	case gcpdetector.AppEngineStandard:
		return "AppEngineStandard"
	case gcpdetector.AppEngineFlex:
		return "AppEngineFlex"
	case gcpdetector.BareMetalSolution:
		return "BareMetalSolution"
	case gcpdetector.UnknownPlatform:
		return "UnknownPlatform"
	default:
		return fmt.Sprintf("unrecognized platform %d", platform)
	}
}

func main() {
	logf("gcp detector probe")
	logf("the metadata client dials with a timeout of 2s, caps a request at 5s and retries up to 5 times")
	logf("")
	logf("== CloudPlatform(), in the order in which it evaluates the platforms ==")
	probeBareMetalSolution()
	probeGKE()
	probeFaaSAndAppEngine()
	probeGCE()

	logf("")
	logf("== what the gcp detector calls after CloudPlatform() ==")
	probeOnGCE()

	logf("")
	logf("== reference measurements ==")
	probeReference()

	logf("")
	logf("== the real detector, end to end ==")
	probeRealDetector()

	logf("")
	logf("probe finished after %s", time.Since(started).Round(time.Millisecond))
}
