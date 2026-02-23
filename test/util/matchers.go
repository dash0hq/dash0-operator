// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/dash0hq/dash0-operator/internal/util"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func MatchEnvVar(name string, value string, args ...any) gomega.OmegaMatcher {
	return &MatchEnvVarMatcher{
		Name:  name,
		Value: value,
		Args:  args,
	}
}

type MatchEnvVarMatcher struct {
	Name  string
	Value string
	Args  []any
}

func (matcher *MatchEnvVarMatcher) Match(actual any) (success bool, err error) {
	envVar, ok := actual.(corev1.EnvVar)
	if !ok {
		return false, fmt.Errorf("MatchEnvVar matcher requires a corev1.EnvVar. Got:\n%s", format.Object(actual, 1))
	}
	return matcher.Name == envVar.Name && matcher.Value == envVar.Value, nil
}

func (matcher *MatchEnvVarMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchEnvVarMatcher) message() string {
	return fmt.Sprintf("to contain env var with name %s and value %s", matcher.Name, matcher.Value)
}

func (matcher *MatchEnvVarMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

type MatchEnvVarValueFromSecretMatcher struct {
	Name       string
	SecretName string
	SecretKey  string
	Args       []any
}

func (matcher *MatchEnvVarValueFromSecretMatcher) Match(actual any) (success bool, err error) {
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

func (matcher *MatchEnvVarValueFromSecretMatcher) FailureMessage(actual any) (message string) {
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

func (matcher *MatchEnvVarValueFromSecretMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchVolume(name string, args ...any) gomega.OmegaMatcher {
	return &MatchVolumeMatcher{
		Name: name,
		Args: args,
	}
}

type MatchVolumeMatcher struct {
	Name string
	Args []any
}

func (matcher *MatchVolumeMatcher) Match(actual any) (success bool, err error) {
	volume, ok := actual.(corev1.Volume)
	if !ok {
		return false, fmt.Errorf(
			"MatchVolume matcher requires a corev1.Volume. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == volume.Name, nil
}

func (matcher *MatchVolumeMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchVolumeMatcher) message() string {
	return fmt.Sprintf("to contain volume with name %s", matcher.Name)
}

func (matcher *MatchVolumeMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchVolumeMount(name string, mountPath string, args ...any) gomega.OmegaMatcher {
	return &MatchVolumeMountMatcher{
		Name:      name,
		MountPath: mountPath,
		Args:      args,
	}
}

type MatchVolumeMountMatcher struct {
	Name      string
	MountPath string
	Args      []any
}

func (matcher *MatchVolumeMountMatcher) Match(actual any) (success bool, err error) {
	volume, ok := actual.(corev1.VolumeMount)
	if !ok {
		return false, fmt.Errorf(
			"MatchVolumeMount matcher requires a corev1.VolumeMount. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == volume.Name && matcher.MountPath == volume.MountPath, nil
}

func (matcher *MatchVolumeMountMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchVolumeMountMatcher) message() string {
	return fmt.Sprintf("to contain volume mount with name %s and mount path %s", matcher.Name, matcher.MountPath)
}

func (matcher *MatchVolumeMountMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchEvent(
	namespace string,
	resourceName string,
	eventType string,
	reason util.Reason,
	action util.Action,
	note string,
	args ...any,
) gomega.OmegaMatcher {
	return &MatchEventMatcher{
		Namespace:    namespace,
		ResourceName: resourceName,
		EventType:    eventType,
		Reason:       reason,
		Action:       action,
		Note:         note,
		Args:         args,
	}
}

type MatchEventMatcher struct {
	Namespace    string
	ResourceName string
	EventType    string
	Reason       util.Reason
	Action       util.Action
	Note         string
	Args         []any
}

func (matcher *MatchEventMatcher) Match(actual any) (success bool, err error) {
	event, ok := actual.(eventsv1.Event)
	if !ok {
		return false, fmt.Errorf(
			"MatchEvent matcher requires a eventsv1.Event. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Namespace == event.ObjectMeta.Namespace &&
			strings.Contains(event.ObjectMeta.Name, matcher.ResourceName) &&
			matcher.EventType == event.Type &&
			string(matcher.Reason) == event.Reason &&
			string(matcher.Action) == event.Action &&
			matcher.Note == event.Note,
		nil
}

func (matcher *MatchEventMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchEventMatcher) message() string {
	return fmt.Sprintf("to contain event with for resource %s/%s, type %s, reason %s, action %s and note %s",
		matcher.Namespace,
		matcher.ResourceName,
		matcher.EventType,
		matcher.Reason,
		matcher.Action,
		matcher.Note,
	)
}

func (matcher *MatchEventMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchServicePort(name string, port int32, targetPort intstr.IntOrString) gomega.OmegaMatcher {
	return &MatchServicePortMatcher{
		Name:       name,
		Port:       port,
		TargetPort: targetPort,
	}
}

type MatchServicePortMatcher struct {
	Name       string
	Port       int32
	TargetPort intstr.IntOrString
}

func (matcher *MatchServicePortMatcher) Match(actual any) (success bool, err error) {
	servicePort, ok := actual.(corev1.ServicePort)
	if !ok {
		return false, fmt.Errorf(
			"MatchServicePort matcher requires a corev1.ServicePort. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == servicePort.Name &&
		matcher.Port == servicePort.Port &&
		matcher.TargetPort == servicePort.TargetPort, nil
}

func (matcher *MatchServicePortMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchServicePortMatcher) message() string {
	return fmt.Sprintf(
		"to contain service port name %s, port %d and targetPort %v",
		matcher.Name, matcher.Port, matcher.TargetPort)
}

func (matcher *MatchServicePortMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}

func MatchContainerPort(name string, port int32) gomega.OmegaMatcher {
	return &MatchContainerPortMatcher{
		Name: name,
		Port: port,
	}
}

type MatchContainerPortMatcher struct {
	Name string
	Port int32
}

func (matcher *MatchContainerPortMatcher) Match(actual any) (success bool, err error) {
	containerPort, ok := actual.(corev1.ContainerPort)
	if !ok {
		return false, fmt.Errorf(
			"MatchContainerPort matcher requires a corev1.ContainerPort. Got:\n%s",
			format.Object(actual, 1))
	}
	return matcher.Name == containerPort.Name && matcher.Port == containerPort.ContainerPort, nil
}

func (matcher *MatchContainerPortMatcher) FailureMessage(actual any) (message string) {
	return format.Message(actual, matcher.message())
}

func (matcher *MatchContainerPortMatcher) message() string {
	return fmt.Sprintf(
		"to contain container port name %s with port %d",
		matcher.Name, matcher.Port)
}

func (matcher *MatchContainerPortMatcher) NegatedFailureMessage(actual any) (message string) {
	return format.Message(actual, fmt.Sprintf("not %s", matcher.message()))
}
