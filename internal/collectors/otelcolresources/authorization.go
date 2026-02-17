// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"errors"
	"fmt"
	"strings"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

const authEnvVarPrefix = "OTELCOL_AUTH_TOKEN"

type dash0ExporterAuthorization struct {
	EnvVarName    string
	Authorization dash0common.Authorization
}

var authEnvVarNameDefault = fmt.Sprintf("%s_DEFAULT", authEnvVarPrefix)

func authEnvVarNameDefaultIndexed(index int) string {
	return fmt.Sprintf("%s_%d", authEnvVarNameDefault, index)
}

func authEnvVarNameForNs(namespace string) string {
	envVarSuffix := strings.ReplaceAll(namespace, "-", "_")
	envVarSuffix = strings.ToUpper(envVarSuffix)

	return fmt.Sprintf("%s_NS_%s", authEnvVarPrefix, envVarSuffix)
}

func authEnvVarNameForNsIndexed(namespace string, index int) string {
	base := authEnvVarNameForNs(namespace)
	return fmt.Sprintf("%s_%d", base, index)
}

func dash0ExporterAuthorizationForExport(export dash0common.Export, index int, isDefault bool, namespace *string) (*dash0ExporterAuthorization, error) {
	if isDefault {
		return &dash0ExporterAuthorization{
			EnvVarName:    authEnvVarNameDefaultIndexed(index),
			Authorization: export.Dash0.Authorization,
		}, nil
	} else {
		if namespace == nil {
			return nil, errors.New("namespace for namespaced Dash0 authorization was nil")
		}
		return &dash0ExporterAuthorization{
			EnvVarName:    authEnvVarNameForNsIndexed(*namespace, index),
			Authorization: export.Dash0.Authorization,
		}, nil
	}
}
