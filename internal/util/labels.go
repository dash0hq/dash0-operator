// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	instrumentedLabelKey = "dash0.com/instrumented"

	// instrumentedLabelValueSuccessful os written by the operator when then instrumentation attempt has been
	// successful.
	instrumentedLabelValueSuccessful instrumentedState = "true"

	// instrumentedLabelValueUnsuccessful is written by the operator when then instrumentation attempt has failed.
	instrumentedLabelValueUnsuccessful instrumentedState = "false"

	// instrumentedLabelValueUnknown should never occur in the wild, it is used as a fallback for inconsistent states.
	// or when the label is missing entirely.
	instrumentedLabelValueUnknown instrumentedState = "unknown"

	dash0EnableLabelKey        = "dash0.com/enable"
	operatorImageLabelKey      = "dash0.com/operator-image"
	initContainerImageLabelKey = "dash0.com/init-container-image"
	instrumentedByLabelKey     = "dash0.com/instrumented-by"
	webhookIgnoreOnceLabelKey  = "dash0.com/webhook-ignore-once"
)

var (
	EmptyListOptions                          = metav1.ListOptions{}
	WorkloadsWithDash0InstrumentedLabelFilter = metav1.ListOptions{
		LabelSelector: instrumentedLabelKey,
	}
)

type instrumentedState string

func AddInstrumentationLabels(
	objectMeta *metav1.ObjectMeta,
	instrumentationSuccess bool,
	clusterInstrumentationConfig ClusterInstrumentationConfig,
	actor WorkloadModifierActor,
) {
	if instrumentationSuccess {
		addLabel(objectMeta, instrumentedLabelKey, string(instrumentedLabelValueSuccessful))
	} else {
		addLabel(objectMeta, instrumentedLabelKey, string(instrumentedLabelValueUnsuccessful))
	}
	addLabel(objectMeta, operatorImageLabelKey, ImageNameToLabel(clusterInstrumentationConfig.OperatorImage))
	addLabel(objectMeta, initContainerImageLabelKey, ImageNameToLabel(clusterInstrumentationConfig.InitContainerImage))
	addLabel(objectMeta, instrumentedByLabelKey, string(actor))
}

func AddWebhookIgnoreOnceLabel(objectMeta *metav1.ObjectMeta) {
	addLabel(objectMeta, webhookIgnoreOnceLabelKey, "true")
}

func addLabel(objectMeta *metav1.ObjectMeta, key string, value string) {
	if objectMeta.Labels == nil {
		objectMeta.Labels = make(map[string]string, 1)
	}
	objectMeta.Labels[key] = value
}

func RemoveInstrumentationLabels(objectMeta *metav1.ObjectMeta) {
	removeLabel(objectMeta, instrumentedLabelKey)
	removeLabel(objectMeta, operatorImageLabelKey)
	removeLabel(objectMeta, initContainerImageLabelKey)
	removeLabel(objectMeta, instrumentedByLabelKey)
}

func removeLabel(objectMeta *metav1.ObjectMeta, key string) {
	delete(objectMeta.Labels, key)
}

func HasBeenInstrumentedSuccessfully(objectMeta *metav1.ObjectMeta) bool {
	return readInstrumentationState(objectMeta) == instrumentedLabelValueSuccessful
}

func HasBeenInstrumentedSuccessfullyByThisVersion(
	objectMeta *metav1.ObjectMeta,
	images Images,
) bool {
	if !HasBeenInstrumentedSuccessfully(objectMeta) {
		return false
	}
	operatorImageValue, operatorImageIsSet := readLabel(objectMeta, operatorImageLabelKey)
	initContainerImageValue, initContainerImageIsSet := readLabel(objectMeta, initContainerImageLabelKey)
	if !operatorImageIsSet || !initContainerImageIsSet {
		return false
	}
	expectedOperatorImageLabel := ImageNameToLabel(images.OperatorImage)
	expectedInitContainerImageLabel := ImageNameToLabel(images.InitContainerImage)
	return operatorImageValue == expectedOperatorImageLabel && initContainerImageValue == expectedInitContainerImageLabel
}

func InstrumentationAttemptHasFailed(objectMeta *metav1.ObjectMeta) bool {
	return readInstrumentationState(objectMeta) == instrumentedLabelValueUnsuccessful
}

func readInstrumentationState(objectMeta *metav1.ObjectMeta) instrumentedState {
	instrumented, isSet := readLabel(objectMeta, instrumentedLabelKey)
	if !isSet {
		return instrumentedLabelValueUnknown
	}
	switch instrumented {
	case string(instrumentedLabelValueSuccessful):
		return instrumentedLabelValueSuccessful
	case string(instrumentedLabelValueUnsuccessful):
		return instrumentedLabelValueUnsuccessful
	default:
		return instrumentedLabelValueUnknown
	}
}

func HasOptedOutOfInstrumentation(objectMeta *metav1.ObjectMeta) bool {
	return hasOptedOutOfInstrumentation(objectMeta)
}

func HasOptedOutOfInstrumentationAndIsUninstrumented(objectMeta *metav1.ObjectMeta) bool {
	return hasOptedOutOfInstrumentation(objectMeta) && !HasBeenInstrumentedSuccessfully(objectMeta)
}

func WasInstrumentedButHasOptedOutNow(objectMeta *metav1.ObjectMeta) bool {
	return HasBeenInstrumentedSuccessfully(objectMeta) && hasOptedOutOfInstrumentation(objectMeta)
}

func hasOptedOutOfInstrumentation(objectMeta *metav1.ObjectMeta) bool {
	dash0EnabledValue, isSet := readLabel(objectMeta, dash0EnableLabelKey)
	return isSet && dash0EnabledValue == "false"
}

func CheckAndDeleteIgnoreOnceLabel(objectMeta *metav1.ObjectMeta) bool {
	if objectMeta.Labels == nil {
		return false
	}
	if value, ok := objectMeta.Labels[webhookIgnoreOnceLabelKey]; ok {
		delete(objectMeta.Labels, webhookIgnoreOnceLabelKey)
		return value == "true"
	}
	return false
}

func ImageNameToLabel(imageName string) string {
	// See https://github.com/distribution/reference/blob/e60f3474a5da95391815dacd158f9dba50ef7df4/regexp.go#L136 ->
	// referencePat for parsing logic for image names, if required. In particular, if we see longer image names out in
	// the wild (due to longer registry names), we might want to prefer the tag/version over the registry name when
	// truncating. For now, we conveniently ignore this problem.
	label :=
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(
					imageName,
					"@", "_",
				),
				"/", "_",
			),
			":", "_",
		)
	if len(label) <= 63 {
		return label
	}
	return label[:63]
}

func readLabel(objectMeta *metav1.ObjectMeta, key string) (string, bool) {
	if objectMeta.Labels == nil {
		return "", false
	}
	if value, ok := objectMeta.Labels[key]; ok {
		return value, true
	}
	return "", false
}
