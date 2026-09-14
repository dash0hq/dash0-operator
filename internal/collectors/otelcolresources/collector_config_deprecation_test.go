// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
)

// This test starts a collector for every configuration of the matrix in collector_config_matrix_test.go and fails when
// the collector logs a deprecation warning while creating the components.
//
// It is the early warning for the failure OPE-568 ended in. The OpenTelemetry collector project does not remove a
// setting without deprecating it first, usually several releases earlier, and it warns about a deprecated setting when
// it creates the component. Failing on that warning turns "the collectors crash-loop after the next collector bump"
// into "CI is red at the release where the deprecation landed", which is months of lead time.
//
// The validate subcommand cannot take this over: it never builds a logger, so it produces no output at all for a
// configuration it accepts. Only an actual collector process logs the warnings.
//
// The collector is not expected to reach a running state here. There is no API server, no kubelet and no export
// endpoint, so components fail to start for reasons that say nothing about the configuration. The deprecation warnings
// are emitted while the components are created, which happens before any of that, so this test only looks at the log
// output and ignores how the process ends.

const (
	// collectorStartupSeconds bounds how long a collector is watched for the outcome of creating its components. It is
	// an upper limit for a container that does not get going at all, not a wait: the collector is stopped as soon as it
	// reports an outcome, which normally takes about a second.
	collectorStartupSeconds = 60

	// componentsCreatedPattern matches the log line with which the collector's service begins to start the components
	// it has created. Everything a component logs while being created, deprecation warnings included, precedes it.
	componentsCreatedPattern = `service\.go:[0-9]+[[:space:]]+Starting `

	// deprecationCheckConcurrency is the number of collectors that run at the same time. They only publish ports
	// inside their own container, so they do not interfere with each other.
	deprecationCheckConcurrency = 8

	// collectorOutcomePrefix marks the last line of a collector's output, which reports whether the collector got far
	// enough for the deprecation warnings of its components to have been logged.
	collectorOutcomePrefix = "DASH0_COLLECTOR_OUTCOME:"
	// collectorOutcomeComponentsCreated means the collector went on to start its components, collectorOutcomeExited
	// that it gave up, which it is expected to do without an API server or an export endpoint. Both mean that all
	// components have been created and have logged whatever they had to say about their configuration.
	collectorOutcomeComponentsCreated = "components-created"
	collectorOutcomeExited            = "exited"
	// collectorOutcomeUndecided means the collector neither started nor exited within collectorStartupSeconds, so its
	// output says nothing about deprecated settings.
	collectorOutcomeUndecided = "undecided"
)

// deprecationWarning matches the log lines that report a deprecated setting or component. The collector has no single
// wording for these, so the match is deliberately broad; anything it catches is worth looking at.
var deprecationWarning = regexp.MustCompile(`(?i)deprecat`)

// knownDeprecation is a deprecation warning that the collector configuration templates currently provoke, together
// with the change that resolves it.
type knownDeprecation struct {
	pattern *regexp.Regexp
	fix     string
}

// knownDeprecations are the deprecation warnings the collector logs for the operator's configurations today. They do
// not fail the test, so that it can guard against new deprecations right away, but every one of them is a setting that
// the OpenTelemetry collector project will remove in one of the next releases - the same situation that made the
// collectors crash-loop in OPE-568, just before the removal. They should be worked off rather than carried along.
//
// A pattern that no longer matches anything fails the test as well, so an entry cannot outlive the deprecation it
// covers.
var knownDeprecations = []knownDeprecation{
	{
		pattern: regexp.MustCompile(`"hostmetrics" alias is deprecated`),
		fix:     "rename the receiver hostmetrics to host_metrics in the collector configuration templates",
	},
	{
		pattern: regexp.MustCompile(`"kubeletstats" alias is deprecated`),
		fix:     "rename the receiver kubeletstats to kubelet_stats in the collector configuration templates",
	},
	{
		pattern: regexp.MustCompile(`"otlp" alias is deprecated`),
		fix:     "rename the exporter otlp to otlp_grpc in the collector configuration templates",
	},
	{
		pattern: regexp.MustCompile(`"resourcedetection" alias is deprecated`),
		fix:     "rename the processor resourcedetection to resource_detection in the collector configuration templates",
	},
	{
		pattern: regexp.MustCompile(`the k8snode detector name is deprecated`),
		fix:     "rename the resourcedetection detector k8snode to k8s_api in the collector configuration templates",
	},
}

type deprecationResult struct {
	name string
	// warnings are the deprecation warnings that no entry of knownDeprecations covers.
	warnings []string
	// knownWarningsSeen indexes the entries of knownDeprecations that this configuration matched.
	knownWarningsSeen map[int]struct{}
	// outcome reports whether the collector got far enough for its output to be meaningful, see
	// collectorOutcomePrefix.
	outcome string
}

func TestCollectorConfigurationsUseNoDeprecatedSettings(t *testing.T) {
	collectorImage := os.Getenv(collectorImageEnvVarName)
	if collectorImage == "" {
		t.Skipf(
			"skipping the deprecation check, set %s to the collector image to check against (or run "+
				"`make collector-config-deprecation-check`)",
			collectorImageEnvVarName,
		)
	}
	RegisterTestingT(t)

	signalControlCollectorImage := os.Getenv(signalControlCollectorImageEnvVarName)
	configurations := renderCollectorConfigurationMatrix(t)
	if signalControlCollectorImage == "" {
		configurations = withoutSignalControlConfigurations(t, configurations)
	}
	t.Logf(
		"starting %d collector configurations with %s and checking their log output for deprecation warnings",
		len(configurations),
		collectorImage,
	)

	results := checkCollectorConfigurationsForDeprecations(
		collectorImage,
		signalControlCollectorImage,
		configurations,
	)

	var failures []string
	var inconclusive []string
	configurationsWithWarnings := 0
	knownWarningsSeen := map[int]struct{}{}
	for _, result := range results {
		for index := range result.knownWarningsSeen {
			knownWarningsSeen[index] = struct{}{}
		}
		if result.outcome != collectorOutcomeComponentsCreated && result.outcome != collectorOutcomeExited {
			inconclusive = append(inconclusive, fmt.Sprintf("%s (%s)", result.name, result.outcome))
		}
		if len(result.warnings) > 0 {
			configurationsWithWarnings++
			failures = append(
				failures,
				fmt.Sprintf("%s:\n  %s", result.name, strings.Join(result.warnings, "\n  ")),
			)
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Errorf(
			"the collector %s logged new deprecation warnings for %d of %d configurations; adapt the collector "+
				"configuration templates now, the settings will be removed in one of the next collector "+
				"releases:\n\n%s",
			collectorImage,
			configurationsWithWarnings,
			len(configurations),
			strings.Join(failures, "\n\n"),
		)
	}

	if len(inconclusive) > 0 {
		sort.Strings(inconclusive)
		t.Errorf(
			"%d of %d collectors neither finished creating their components nor exited within %d seconds, so their "+
				"output says nothing about deprecated settings:\n  %s",
			len(inconclusive),
			len(configurations),
			collectorStartupSeconds,
			strings.Join(inconclusive, "\n  "),
		)
	}

	for index, known := range knownDeprecations {
		if _, seen := knownWarningsSeen[index]; !seen {
			t.Errorf(
				"the collector %s no longer logs a deprecation warning matching %s; remove that entry from "+
					"knownDeprecations",
				collectorImage,
				known.pattern,
			)
		}
	}
}

func withoutSignalControlConfigurations(
	t *testing.T,
	configurations []renderedCollectorConfig,
) []renderedCollectorConfig {
	t.Helper()

	remaining := make([]renderedCollectorConfig, 0, len(configurations))
	skipped := 0
	for _, configuration := range configurations {
		if configuration.signalControlCollector {
			skipped++
			continue
		}
		remaining = append(remaining, configuration)
	}
	t.Logf(
		"skipping %d Signal Control collector configurations, set %s to check them as well",
		skipped,
		signalControlCollectorImageEnvVarName,
	)
	return remaining
}

func checkCollectorConfigurationsForDeprecations(
	collectorImage string,
	signalControlCollectorImage string,
	configurations []renderedCollectorConfig,
) []deprecationResult {
	results := make([]deprecationResult, len(configurations))
	semaphore := make(chan struct{}, deprecationCheckConcurrency)
	var waitGroup sync.WaitGroup

	for i, configuration := range configurations {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			image := collectorImage
			if configuration.signalControlCollector {
				image = signalControlCollectorImage
			}
			// The collector is expected to fail to start in this environment, so its exit status is ignored; only its
			// log output matters.
			output, _ := startCollectorWithConfiguration(image, configuration)
			warnings, knownWarningsSeen := deprecationWarningsIn(output)
			results[i] = deprecationResult{
				name:              configuration.name,
				warnings:          warnings,
				knownWarningsSeen: knownWarningsSeen,
				outcome:           collectorOutcomeIn(output),
			}
		}()
	}

	waitGroup.Wait()
	return results
}

// startCollectorWithConfiguration starts a collector, waits until it has created its components and stops it again,
// returning everything it logged.
//
// Waiting for a fixed amount of time would make the result depend on how quickly the container happens to start. The
// collector is therefore watched until it reports either outcome of creating its components - it is running, or it
// gave up - and the last line of the output says which of the two happened, so that a collector that reached neither
// is reported as a failure instead of silently passing without any log output to inspect.
func startCollectorWithConfiguration(
	collectorImage string,
	configuration renderedCollectorConfig,
) (string, error) {
	script := collectorContainerPreamble(configuration.content)
	script += "cat > /tmp/config.yaml\n"
	// set -e from the preamble must not end the script when the collector exits with an error, which it is expected to
	// do in an environment without an API server or an export endpoint.
	script += "set +e\n"
	script += fmt.Sprintf("/otelcol %s > /tmp/collector.log 2>&1 &\n", collectorArguments(configuration))
	script += "COLLECTOR_PID=$!\n"
	script += fmt.Sprintf("WAITED=0\nOUTCOME=%s\n", collectorOutcomeUndecided)
	script += fmt.Sprintf("while [ $WAITED -lt %d ]; do\n", collectorStartupSeconds)
	script += "  if ! kill -0 $COLLECTOR_PID 2> /dev/null; then\n"
	script += fmt.Sprintf("    OUTCOME=%s\n    break\n", collectorOutcomeExited)
	script += "  fi\n"
	script += fmt.Sprintf("  if grep -qE '%s' /tmp/collector.log 2> /dev/null; then\n", componentsCreatedPattern)
	script += fmt.Sprintf("    OUTCOME=%s\n    break\n", collectorOutcomeComponentsCreated)
	script += "  fi\n"
	script += "  sleep 1\n"
	script += "  WAITED=$((WAITED+1))\n"
	script += "done\n"
	script += "kill $COLLECTOR_PID 2> /dev/null\n"
	script += "wait $COLLECTOR_PID 2> /dev/null\n"
	script += "cat /tmp/collector.log\n"
	script += fmt.Sprintf("echo \"%s$OUTCOME\"\n", collectorOutcomePrefix)
	script += "exit 0\n"
	return runCollectorContainer(collectorImage, configuration, script)
}

// collectorOutcomeIn reads the outcome that startCollectorWithConfiguration appends to a collector's output.
func collectorOutcomeIn(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if outcome, found := strings.CutPrefix(line, collectorOutcomePrefix); found {
			return outcome
		}
	}
	return "no outcome reported"
}

// deprecationWarningsIn splits the deprecation warnings of a collector's log output into the ones that are not covered
// by knownDeprecations and the indexes of the entries of knownDeprecations that were matched.
func deprecationWarningsIn(output string) ([]string, map[int]struct{}) {
	var warnings []string
	knownWarningsSeen := map[int]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		if !deprecationWarning.MatchString(line) {
			continue
		}
		known := false
		for index, knownWarning := range knownDeprecations {
			if knownWarning.pattern.MatchString(line) {
				knownWarningsSeen[index] = struct{}{}
				known = true
				break
			}
		}
		if !known {
			warnings = append(warnings, strings.TrimSpace(line))
		}
	}
	return warnings, knownWarningsSeen
}
