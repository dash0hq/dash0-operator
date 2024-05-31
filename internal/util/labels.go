// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	instrumentedLabelKey = "dash0.instrumented"

	// InstrumentedLabelValueSuccessful os written by the operator when then instrumentation attempt has been
	// successful.
	instrumentedLabelValueSuccessful instrumentedState = "successful"

	// InstrumentedLabelValueUnsuccessful is written by the operator when then instrumentation attempt has failed.
	instrumentedLabelValueUnsuccessful instrumentedState = "unsuccessful"

	// InstrumentedLabelValueOptOut is never written by the operator, this can be set externally to opt-out of
	// instrumentation for a particular workload
	instrumentedLabelValueOptOut instrumentedState = "false"

	// InstrumentedLabelValueUnknown should never occur in the wild, it is used as a fallback for inconsistent states
	// or when the label is missing entirely.
	instrumentedLabelValueUnknown instrumentedState = "unknown"

	operatorVersionLabelKey           = "dash0.operator.version"
	initContainerImageVersionLabelKey = "dash0.initcontainer.image.version"
	instrumentedByLabelKey            = "dash0.instrumented.by"
	webhookIgnoreOnceLabelKey         = "dash0.webhook.ignore.once"
)

var (
	WorkloadsWithoutDash0InstrumentedLabelFilter = metav1.ListOptions{
		LabelSelector: fmt.Sprintf("!%s", instrumentedLabelKey),
	}
	WorkloadsWithDash0InstrumentedLabelExlucdingOptOutFilter = metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%[1]s,%[1]s != false", instrumentedLabelKey),
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
	if meta.GetLabels()[instrumentedLabelKey] != "false" {
		removeLabel(meta, instrumentedLabelKey)
	}
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

func HasOptedOutOfInstrumenation(meta *metav1.ObjectMeta) bool {
	return readInstrumentationState(meta) == instrumentedLabelValueOptOut
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
	case string(instrumentedLabelValueOptOut):
		return instrumentedLabelValueOptOut
	default:
		return instrumentedLabelValueUnknown
	}
}

func CheckAndDeleteIgnoreOnceLabel(meta *metav1.ObjectMeta) bool {
	if meta.Labels == nil {
		return false
	}
	if value, ok := meta.Labels[webhookIgnoreOnceLabelKey]; ok && value == "true" {
		delete(meta.Labels, webhookIgnoreOnceLabelKey)
		return true
	}
	return false
}
