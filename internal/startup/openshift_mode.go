// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"flag"
	"os"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	openShiftFlagName = "dash0-openshift"

	// podOpenShiftSccEnvVarName holds the value of the openshift.io/scc annotation of the operator manager pod, provided
	// via the downward API. It is empty when the pod has not been admitted by SecurityContextConstraints, since the pod
	// then has no such annotation.
	podOpenShiftSccEnvVarName = "K8S_POD_OPENSHIFT_SCC"
)

// resolveOpenShiftMode returns the value of --dash0-openshift if the flag has been set explicitly, either as a
// command-line argument or via its environment variable DASH0_OPENSHIFT. Otherwise, OpenShift mode is enabled if the
// operator manager pod has been admitted by OpenShift's SecurityContextConstraints admission, which sets the
// openshift.io/scc annotation on every pod it admits. The SCC admission is what makes pinned UIDs fail, so the
// annotation is a more precise signal than the existence of OpenShift-specific API groups.
func resolveOpenShiftMode(fs *flag.FlagSet, flagValue bool, logger logd.Logger) bool {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == openShiftFlagName {
			explicit = true
		}
	})
	if explicit {
		logger.Info(
			"OpenShift mode has been set explicitly via --dash0-openshift or DASH0_OPENSHIFT.",
			"enabled", flagValue,
		)
		return flagValue
	}

	scc := os.Getenv(podOpenShiftSccEnvVarName)
	if scc != "" {
		logger.Info(
			"OpenShift mode has been enabled automatically, since the operator manager pod has been admitted by "+
				"OpenShift's SecurityContextConstraints.",
			"securityContextConstraints", scc,
		)
		return true
	}
	logger.Info(
		"OpenShift mode is disabled, since the operator manager pod has not been admitted by OpenShift's " +
			"SecurityContextConstraints.",
	)
	return false
}
