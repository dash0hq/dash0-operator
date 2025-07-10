// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

const (
	OtelServiceNameEnvVarName        = "OTEL_SERVICE_NAME"
	OtelResourceAttributesEnvVarName = "OTEL_RESOURCE_ATTRIBUTES"
)

func CreateEnvVarForAuthorization(
	dash0Authorization dash0v1alpha1.Authorization,
	envVarName string,
) (corev1.EnvVar, error) {
	token := dash0Authorization.Token
	secretRef := dash0Authorization.SecretRef
	if token != nil && *token != "" {
		return corev1.EnvVar{
			Name:  envVarName,
			Value: *token,
		}, nil
	} else if secretRef != nil && secretRef.Name != "" && secretRef.Key != "" {
		return corev1.EnvVar{
			Name: envVarName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretRef.Name,
					},
					Key: secretRef.Key,
				},
			},
		}, nil
	} else {
		return corev1.EnvVar{}, fmt.Errorf("neither token nor secretRef provided for the Dash0 exporter")
	}
}
