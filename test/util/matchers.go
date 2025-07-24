// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/dash0hq/dash0-operator/internal/util"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
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
		return false, fmt.Errorf("MatchEnvVar matcher requires a corev1.EnvVar. Got:\n%s", format.Object(actual, 1))
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

type MatchEnvVarValueFromSecretMatcher struct {
	Name       string
	SecretName string
	SecretKey  string
	Args       []interface{}
}

func (matcher *MatchEnvVarValueFromSecretMatcher) Match(actual interface{}) (success bool, err error) {
	envVar, ok := actual.(corev1.EnvVar)
	if !ok {
		return false,
			fmt.Errorf(
				"MatchEnvVarValueFromSecretMatcher matcher requires a corev1.EnvVar. Got:\n%s",
				format.Object(actual, 1),
			)
	}
	return matcher.Name == envVar.Name &&
			envVar.ValueFrom != nil &&
			envVar.ValueFrom.SecretKeyRef != nil &&
			matcher.SecretName == envVar.ValueFrom.SecretKeyRef.Name &&
			matcher.SecretKey == envVar.ValueFrom.SecretKeyRef.Key,
		nil
}

func (matcher *MatchEnvVarValueFromSecretMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchEnvVarValueFromSecretMatcher) message() string {
	return fmt.Sprintf(
		"to contain env var with name %s and value from secret %s/%s",
		matcher.Name,
		matcher.SecretName,
		matcher.SecretKey,
	)
}

func (matcher *MatchEnvVarValueFromSecretMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchVolume(name string, args ...interface{}) gomega.OmegaMatcher {
	return &MatchVolumeMatcher{
		Name: name,
		Args: args,
	}
}

type MatchVolumeMatcher struct {
	Name string
	Args []interface{}
}

func (matcher *MatchVolumeMatcher) Match(actual interface{}) (success bool, err error) {
	volume, ok := actual.(corev1.Volume)
	if !ok {
		return false, fmt.Errorf(
			"MatchVolume matcher requires a corev1.Volume. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == volume.Name, nil
}

func (matcher *MatchVolumeMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchVolumeMatcher) message() string {
	return fmt.Sprintf("to contain volume with name %s", matcher.Name)
}

func (matcher *MatchVolumeMatcher) NegatedFailureMessage(actual interface{}) (message string) {
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
	volume, ok := actual.(corev1.VolumeMount)
	if !ok {
		return false, fmt.Errorf(
			"MatchVolumeMount matcher requires a corev1.VolumeMount. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == volume.Name && matcher.MountPath == volume.MountPath, nil
}

func (matcher *MatchVolumeMountMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchVolumeMountMatcher) message() string {
	return fmt.Sprintf("to contain volume mount with name %s and mount path %s", matcher.Name, matcher.MountPath)
}

func (matcher *MatchVolumeMountMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchEvent(
	namespace string,
	resourceName string,
	reason util.Reason,
	message string,
	args ...interface{},
) gomega.OmegaMatcher {
	return &MatchEventMatcher{
		Namespace:    namespace,
		ResourceName: resourceName,
		Reason:       string(reason),
		Message:      message,
		Args:         args,
	}
}

type MatchEventMatcher struct {
	Namespace    string
	ResourceName string
	Reason       string
	Message      string
	Args         []interface{}
}

func (matcher *MatchEventMatcher) Match(actual interface{}) (success bool, err error) {
	event, ok := actual.(corev1.Event)
	if !ok {
		return false, fmt.Errorf(
			"MatchEvent matcher requires a corev1.Event. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Namespace == event.ObjectMeta.Namespace &&
			strings.Contains(event.ObjectMeta.Name, matcher.ResourceName) &&
			matcher.Reason == event.Reason &&
			matcher.Message == event.Message,
		nil
}

func (matcher *MatchEventMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchEventMatcher) message() string {
	return fmt.Sprintf("to contain event with for resource %s/%sreason %s and message %s",
		matcher.Namespace,
		matcher.ResourceName,
		matcher.Reason,
		matcher.Message,
	)
}

func (matcher *MatchEventMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}
