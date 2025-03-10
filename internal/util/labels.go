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
	meta *metav1.ObjectMeta,
	instrumentationSuccess bool,
	instrumentationMetadata InstrumentationMetadata,
) {
	if instrumentationSuccess {
		addLabel(meta, instrumentedLabelKey, string(instrumentedLabelValueSuccessful))
	} else {
		addLabel(meta, instrumentedLabelKey, string(instrumentedLabelValueUnsuccessful))
	}
	addLabel(meta, operatorImageLabelKey, ImageNameToLabel(instrumentationMetadata.OperatorImage))
	addLabel(meta, initContainerImageLabelKey, ImageNameToLabel(instrumentationMetadata.InitContainerImage))
	addLabel(meta, instrumentedByLabelKey, string(instrumentationMetadata.InstrumentedBy))
}

func AddWebhookIgnoreOnceLabel(meta *metav1.ObjectMeta) {
	addLabel(meta, webhookIgnoreOnceLabelKey, "true")
}

func addLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}
	meta.Labels[key] = value
}

func RemoveInstrumentationLabels(meta *metav1.ObjectMeta) {
	removeLabel(meta, instrumentedLabelKey)
	removeLabel(meta, operatorImageLabelKey)
	removeLabel(meta, initContainerImageLabelKey)
	removeLabel(meta, instrumentedByLabelKey)
}

func removeLabel(meta *metav1.ObjectMeta, key string) {
	delete(meta.Labels, key)
}

func HasBeenInstrumentedSuccessfully(meta *metav1.ObjectMeta) bool {
	return readInstrumentationState(meta) == instrumentedLabelValueSuccessful
}

func HasBeenInstrumentedSuccessfullyByThisVersion(
	meta *metav1.ObjectMeta,
	images Images,
) bool {
	if !HasBeenInstrumentedSuccessfully(meta) {
		return false
	}
	operatorImageValue, operatorImageIsSet := readLabel(meta, operatorImageLabelKey)
	initContainerImageValue, initContainerImageIsSet := readLabel(meta, initContainerImageLabelKey)
	if !operatorImageIsSet || !initContainerImageIsSet {
		return false
	}
	expectedOperatorImageLabel := ImageNameToLabel(images.OperatorImage)
	expectedInitContainerImageLabel := ImageNameToLabel(images.InitContainerImage)
	return operatorImageValue == expectedOperatorImageLabel && initContainerImageValue == expectedInitContainerImageLabel
}

func InstrumentationAttemptHasFailed(meta *metav1.ObjectMeta) bool {
	return readInstrumentationState(meta) == instrumentedLabelValueUnsuccessful
}

func readInstrumentationState(meta *metav1.ObjectMeta) instrumentedState {
	instrumented, isSet := readLabel(meta, instrumentedLabelKey)
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

func HasOptedOutOfInstrumentation(meta *metav1.ObjectMeta) bool {
	return hasOptedOutOfInstrumentation(meta)
}

func HasOptedOutOfInstrumentationAndIsUninstrumented(meta *metav1.ObjectMeta) bool {
	return hasOptedOutOfInstrumentation(meta) && !HasBeenInstrumentedSuccessfully(meta)
}

func WasInstrumentedButHasOptedOutNow(meta *metav1.ObjectMeta) bool {
	return HasBeenInstrumentedSuccessfully(meta) && hasOptedOutOfInstrumentation(meta)
}

func hasOptedOutOfInstrumentation(meta *metav1.ObjectMeta) bool {
	dash0EnabledValue, isSet := readLabel(meta, dash0EnableLabelKey)
	return isSet && dash0EnabledValue == "false"
}

func CheckAndDeleteIgnoreOnceLabel(meta *metav1.ObjectMeta) bool {
	if meta.Labels == nil {
		return false
	}
	if value, ok := meta.Labels[webhookIgnoreOnceLabelKey]; ok {
		delete(meta.Labels, webhookIgnoreOnceLabelKey)
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

func readLabel(meta *metav1.ObjectMeta, key string) (string, bool) {
	if meta.Labels == nil {
		return "", false
	}
	if value, ok := meta.Labels[key]; ok {
		return value, true
	}
	return "", false
}
