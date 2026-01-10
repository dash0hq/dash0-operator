// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"strings"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

const authEnvVarPrefix = "OTELCOL_AUTH_TOKEN"

var authEnvVarNameDefault = fmt.Sprintf("%s_DEFAULT", authEnvVarPrefix)

type dash0ExporterAuthorization struct {
	EnvVarName    string
	Authorization dash0common.Authorization
}

type dash0ExporterAuthorizationByNamespace = map[string]dash0ExporterAuthorization

type dash0ExporterAuthorizations struct {
	DefaultDash0ExporterAuthorization     *dash0ExporterAuthorization
	NamespacedDash0ExporterAuthorizations dash0ExporterAuthorizationByNamespace
}

func (auths dash0ExporterAuthorizations) all() []dash0ExporterAuthorization {
	allAuths := make([]dash0ExporterAuthorization, 0)
	if auths.DefaultDash0ExporterAuthorization != nil {
		allAuths = append(allAuths, *auths.DefaultDash0ExporterAuthorization)
	}
	for _, auth := range auths.NamespacedDash0ExporterAuthorizations {
		allAuths = append(allAuths, auth)
	}
	return allAuths
}

func authEnvVarNameForNs(namespace string) string {
	envVarSuffix := strings.ReplaceAll(namespace, "-", "_")
	envVarSuffix = strings.ToUpper(envVarSuffix)

	return fmt.Sprintf("%s_NS_%s", authEnvVarPrefix, envVarSuffix)
}

func collectDash0ExporterAuthorizations(operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1beta1.Dash0Monitoring) dash0ExporterAuthorizations {
	var defaultAuth *dash0ExporterAuthorization = nil
	if operatorConfigurationResource != nil && operatorConfigurationResource.Spec.Export != nil && operatorConfigurationResource.Spec.Export.Dash0 != nil {
		defaultAuth = &dash0ExporterAuthorization{
			EnvVarName:    authEnvVarNameDefault,
			Authorization: operatorConfigurationResource.Spec.Export.Dash0.Authorization,
		}
	}
	nsAuths := make(dash0ExporterAuthorizationByNamespace)
	for _, m := range allMonitoringResources {
		if m.Spec.Export != nil && m.Spec.Export.Dash0 != nil {
			nsAuths[m.Namespace] = dash0ExporterAuthorization{
				EnvVarName:    authEnvVarNameForNs(m.Namespace),
				Authorization: m.Spec.Export.Dash0.Authorization,
			}
		}
	}
	return dash0ExporterAuthorizations{
		DefaultDash0ExporterAuthorization:     defaultAuth,
		NamespacedDash0ExporterAuthorizations: nsAuths,
	}
}
