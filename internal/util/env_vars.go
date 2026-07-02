// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

const (
	OtelServiceNameEnvVarName        = "OTEL_SERVICE_NAME"
	OtelResourceAttributesEnvVarName = "OTEL_RESOURCE_ATTRIBUTES"
	OtelPropagatorsEnvVarName        = "OTEL_PROPAGATORS"
)

func CreateEnvVarForAuthorization(
	dash0Authorization dash0common.Authorization,
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

// CreateEnvVarForSecretKeySelector creates an environment variable that sources its value from the given Kubernetes
// secret. This is used to inject header values that are sourced from a secret into the collector pod, which are then
// referenced from the collector configuration via ${env:envVarName}.
func CreateEnvVarForSecretKeySelector(
	secretKeyRef *dash0common.SecretKeySelector,
	envVarName string,
) (corev1.EnvVar, error) {
	if secretKeyRef == nil || secretKeyRef.Name == "" || secretKeyRef.Key == "" {
		return corev1.EnvVar{}, fmt.Errorf("no valid secretKeyRef (name and key) provided for env var %s", envVarName)
	}
	return corev1.EnvVar{
		Name: envVarName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretKeyRef.Name,
				},
				Key: secretKeyRef.Key,
			},
		},
	}, nil
}

func GetEnvVar(container *corev1.Container, name string) *corev1.EnvVar {
	if container == nil {
		return nil
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})
	if idx >= 0 {
		return &container.Env[idx]
	}
	return nil
}

func IsEnvVarUnsetOrEmpty(envVar *corev1.EnvVar) bool {
	if envVar == nil {
		return true
	}
	if envVar.ValueFrom != nil {
		return false
	}
	return strings.TrimSpace(envVar.Value) == ""
}
