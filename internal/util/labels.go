// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

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

	optOutLabelKey                    = "dash0.com/opt-out"
	operatorVersionLabelKey           = "dash0.com/operator-version"
	initContainerImageVersionLabelKey = "dash0.com/init-container-image-version"
	instrumentedByLabelKey            = "dash0.com/instrumented-by"
	webhookIgnoreOnceLabelKey         = "dash0.com/webhook-ignore-once"
)

var (
	WorkloadsWithoutDash0InstrumentedLabelFilter = metav1.ListOptions{
		LabelSelector: fmt.Sprintf("!%s,%s != true", instrumentedLabelKey, optOutLabelKey),
	}
	WorkloadsWithDash0InstrumentedLabelFilter = metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s,%s != true", instrumentedLabelKey, optOutLabelKey),
	}
)

type instrumentedState string

func AddInstrumentationLabels(
	meta *metav1.ObjectMeta,
	instrumenationSuccess bool,
	instrumentationMetadata InstrumentationMetadata,
) {
	if instrumenationSuccess {
		addLabel(meta, instrumentedLabelKey, string(instrumentedLabelValueSuccessful))
	} else {
		addLabel(meta, instrumentedLabelKey, string(instrumentedLabelValueUnsuccessful))
	}
	addLabel(meta, operatorVersionLabelKey, instrumentationMetadata.OperatorVersion)
	addLabel(meta, initContainerImageVersionLabelKey, instrumentationMetadata.InitContainerImageVersion)
	addLabel(meta, instrumentedByLabelKey, instrumentationMetadata.InstrumentedBy)
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
	removeLabel(meta, operatorVersionLabelKey)
	removeLabel(meta, initContainerImageVersionLabelKey)
	removeLabel(meta, instrumentedByLabelKey)
}

func removeLabel(meta *metav1.ObjectMeta, key string) {
	delete(meta.Labels, key)
}

func HasBeenInstrumentedSuccessfully(meta *metav1.ObjectMeta) bool {
	return readInstrumentationState(meta) == instrumentedLabelValueSuccessful
}

func InstrumenationAttemptHasFailed(meta *metav1.ObjectMeta) bool {
	return readInstrumentationState(meta) == instrumentedLabelValueUnsuccessful
}

func readInstrumentationState(meta *metav1.ObjectMeta) instrumentedState {
	if meta.Labels == nil {
		return instrumentedLabelValueUnknown
	}
	switch meta.Labels[instrumentedLabelKey] {
	case string(instrumentedLabelValueSuccessful):
		return instrumentedLabelValueSuccessful
	case string(instrumentedLabelValueUnsuccessful):
		return instrumentedLabelValueUnsuccessful
	default:
		return instrumentedLabelValueUnknown
	}
}

func HasOptedOutOfInstrumenation(meta *metav1.ObjectMeta) bool {
	if meta.Labels == nil {
		return false
	}
	if value, ok := meta.Labels[optOutLabelKey]; ok && value == "true" {
		return true
	}
	return false
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
