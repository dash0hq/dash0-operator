// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// appliedEnvVar records that a command-line flag was set from an environment variable rather than from an explicit
// command-line argument.
type appliedEnvVar struct {
	flag   string
	envVar string
	value  string
}

// envVarNameForFlag derives the environment variable that acts as a fallback for a command-line flag. The "dash0-"
// prefix is trimmed before the "DASH0_" prefix is added, so --dash0-log-level maps to DASH0_LOG_LEVEL rather than
// DASH0_DASH0_LOG_LEVEL; every other flag simply gains the DASH0_ prefix.
func envVarNameForFlag(flagName string) string {
	return "DASH0_" + strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(flagName, "dash0-"), "-", "_"))
}

// applyEnvironmentVariableDefaults fills every flag in fs that was not set on the command line from its corresponding
// DASH0_* environment variable (see envVarNameForFlag). This lets an OLM Subscription, which can inject env vars but not
// container args, configure the full flag surface. Explicit command-line arguments always win. Flags registered by
// controller-runtime's zap options (prefixed "zap-") are excluded, and an environment variable that is unset or empty
// is treated as absent. It returns the applied (flag, envVar, value) triples so the caller can log them once logging is
// configured, and an error if a value could not be parsed by its flag, so a misconfiguration fails fast.
func applyEnvironmentVariableDefaults(fs *flag.FlagSet) ([]appliedEnvVar, error) {
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	var applied []appliedEnvVar
	var setErr error
	fs.VisitAll(func(f *flag.Flag) {
		if setErr != nil || explicit[f.Name] || strings.HasPrefix(f.Name, "zap-") {
			return
		}
		envVar := envVarNameForFlag(f.Name)
		value, ok := os.LookupEnv(envVar)
		if !ok || value == "" {
			return
		}
		if err := fs.Set(f.Name, value); err != nil {
			setErr = fmt.Errorf("cannot apply environment variable %s to flag --%s: %w", envVar, f.Name, err)
			return
		}
		applied = append(applied, appliedEnvVar{flag: f.Name, envVar: envVar, value: value})
	})
	if setErr != nil {
		return nil, setErr
	}
	return applied, nil
}

// logAppliedEnvVarDefaults logs, at debug level, every flag that was set from an environment variable. The resolved
// value of every flag, regardless of whether it came from an argument or the environment, is logged separately by the
// operator manager configuration summary; this line only adds the provenance for troubleshooting.
func logAppliedEnvVarDefaults(applied []appliedEnvVar) {
	for _, a := range applied {
		setupLog.Debug(
			"command-line flag set from environment variable",
			"flag", a.flag,
			"envVar", a.envVar,
			"value", a.loggableValue(),
		)
	}
}

// loggableValue returns the environment variable value for logging, redacting it when the flag name indicates a
// credential (currently only operator-configuration-token).
func (a appliedEnvVar) loggableValue() string {
	if strings.Contains(a.flag, "token") {
		return "(redacted)"
	}
	return a.value
}
