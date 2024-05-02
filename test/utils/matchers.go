// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"github.com/onsi/gomega/format"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func MatchEnvVar(name string, value string, args ...interface{}) gomega.OmegaMatcher {
	return &MatchEnvVarMatcher{
		Name:  name,
		Value: value,
		Args:  args,
	}
}

type MatchEnvVarMatcher struct {
	Name  string
	Value string
	Args  []interface{}
}

func (matcher *MatchEnvVarMatcher) Match(actual interface{}) (success bool, err error) {
	envVar, ok := actual.(corev1.EnvVar)
	if !ok {
		return false, fmt.Errorf("ContainsEnvVar matcher requires a corev1.EnvVar.  Got:\n%s", format.Object(actual, 1))
	}
	return matcher.Name == envVar.Name && matcher.Value == envVar.Value, nil
}

func (matcher *MatchEnvVarMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchEnvVarMatcher) message() string {
	return fmt.Sprintf("to contain env var with name %s and value %s", matcher.Name, matcher.Value)
}

func (matcher *MatchEnvVarMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchVolumeMount(name string, mountPath string, args ...interface{}) gomega.OmegaMatcher {
	return &MatchVolumeMountMatcher{
		Name:      name,
		MountPath: mountPath,
		Args:      args,
	}
}

type MatchVolumeMountMatcher struct {
	Name      string
	MountPath string
	Args      []interface{}
}

func (matcher *MatchVolumeMountMatcher) Match(actual interface{}) (success bool, err error) {
	volumeMount, ok := actual.(corev1.VolumeMount)
	if !ok {
		return false, fmt.Errorf("ContainsVolumeMount matcher requires a corev1.VolumeMount.  Got:\n%s", format.Object(actual, 1))
	}
	return matcher.Name == volumeMount.Name && matcher.MountPath == volumeMount.MountPath, nil
}

func (matcher *MatchVolumeMountMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchVolumeMountMatcher) message() string {
	return fmt.Sprintf("to contain mount volume with name %s and mount path %s", matcher.Name, matcher.MountPath)
}

func (matcher *MatchVolumeMountMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}
