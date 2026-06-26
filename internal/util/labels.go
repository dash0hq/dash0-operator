// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"maps"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

const (
	InstrumentedLabelKey = "dash0.com/instrumented"

	// instrumentedLabelValueSuccessful os written by the operator when then instrumentation attempt has been
	// successful.
	instrumentedLabelValueSuccessful instrumentedState = "true"

	// instrumentedLabelValueUnsuccessful is written by the operator when then instrumentation attempt has failed.
	instrumentedLabelValueUnsuccessful instrumentedState = "false"

	// instrumentedLabelValueUnknown should never occur in the wild, it is used as a fallback for inconsistent states.
	// or when the label is missing entirely.
	instrumentedLabelValueUnknown instrumentedState = "unknown"

	operatorImageLabelKey                = "dash0.com/operator-image"
	instrumentationImageLabelKey         = "dash0.com/instrumentation-image"
	legacyInitContainerImageLabelKey     = "dash0.com/init-container-image"
	instrumentedByLabelKey               = "dash0.com/instrumented-by"
	instrumentationDeliveryAnnotationKey = "dash0.com/instrumentation-delivery"
	webhookIgnoreOnceLabelKey            = "dash0.com/webhook-ignore-once"
	// TODO move operator-image, instrumentation-image and instrumented-by to annotations (later)
)

type instrumentedState string

func AddInstrumentationLabelsAndAnnotations(
	objectMeta *metav1.ObjectMeta,
	instrumentationSuccess bool,
	clusterInstrumentationConfig *ClusterInstrumentationConfig,
	actor WorkloadModifierActor,
) {
	if instrumentationSuccess {
		addLabel(objectMeta, InstrumentedLabelKey, string(instrumentedLabelValueSuccessful))
	} else {
		addLabel(objectMeta, InstrumentedLabelKey, string(instrumentedLabelValueUnsuccessful))
	}
	addLabel(objectMeta, operatorImageLabelKey, ImageRefToLabel(clusterInstrumentationConfig.OperatorImage))
	addLabel(objectMeta, instrumentationImageLabelKey, ImageRefToLabel(clusterInstrumentationConfig.InitContainerImage))
	addLabel(objectMeta, instrumentedByLabelKey, string(actor))
	addAnnotation(
		objectMeta,
		instrumentationDeliveryAnnotationKey,
		string(clusterInstrumentationConfig.GetInstrumentationDelivery()),
	)
	removeLabel(objectMeta, legacyInitContainerImageLabelKey)
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

func addAnnotation(objectMeta *metav1.ObjectMeta, key string, value string) {
	if objectMeta.Annotations == nil {
		objectMeta.Annotations = make(map[string]string, 1)
	}
	objectMeta.Annotations[key] = value
}

func RemoveInstrumentationLabelsAndAnnotations(objectMeta *metav1.ObjectMeta) {
	removeLabel(objectMeta, InstrumentedLabelKey)
	removeLabel(objectMeta, operatorImageLabelKey)
	removeLabel(objectMeta, instrumentationImageLabelKey)
	removeLabel(objectMeta, instrumentedByLabelKey)
	removeAnnotation(objectMeta, instrumentationDeliveryAnnotationKey)
	removeLabel(objectMeta, legacyInitContainerImageLabelKey)
}

func removeLabel(objectMeta *metav1.ObjectMeta, key string) {
	delete(objectMeta.Labels, key)
}

func removeAnnotation(objectMeta *metav1.ObjectMeta, key string) {
	delete(objectMeta.Annotations, key)
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
	instrumentationImageValue, instrumentationImageIsSet := readLabel(objectMeta, instrumentationImageLabelKey)
	if !operatorImageIsSet || !instrumentationImageIsSet {
		return false
	}
	expectedOperatorImageLabel := ImageRefToLabel(images.OperatorImage)
	expectedInitContainerImageLabel := ImageRefToLabel(images.InitContainerImage)
	return operatorImageValue == expectedOperatorImageLabel && instrumentationImageValue == expectedInitContainerImageLabel
}

func InstrumentationAttemptHasFailed(objectMeta *metav1.ObjectMeta) bool {
	return readInstrumentationState(objectMeta) == instrumentedLabelValueUnsuccessful
}

func readInstrumentationState(objectMeta *metav1.ObjectMeta) instrumentedState {
	instrumented, isSet := readLabel(objectMeta, InstrumentedLabelKey)
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

func HasOptedOutOfInstrumentation(objectMeta *metav1.ObjectMeta, autoInstrumentationLabelSelector string) bool {
	return hasOptedOutOfInstrumentation(objectMeta, autoInstrumentationLabelSelector)
}

func HasOptedOutOfInstrumentationAndIsUninstrumented(objectMeta *metav1.ObjectMeta, autoInstrumentationLabelSelector string) bool {
	return hasOptedOutOfInstrumentation(objectMeta, autoInstrumentationLabelSelector) && !HasBeenInstrumentedSuccessfully(objectMeta)
}

func WasInstrumentedButHasOptedOutNow(objectMeta *metav1.ObjectMeta, autoInstrumentationLabelSelector string) bool {
	return HasBeenInstrumentedSuccessfully(objectMeta) && hasOptedOutOfInstrumentation(objectMeta, autoInstrumentationLabelSelector)
}

func hasOptedOutOfInstrumentation(objectMeta *metav1.ObjectMeta, autoInstrumentationLabelSelector string) bool {
	return !shouldBeInstrumented(objectMeta, autoInstrumentationLabelSelector)
}

// shouldBeInstrumented checks whether the workload should be instrumented, that is, whether it has opted in to
// instrumentation via a label, or not actively opted out of instrumentation. This condition can often be implicit: if
// the instrumentation label selector uses the != operator (as the default selector dash0.com/enable!=false does), then
// the absence of that label means that the label selector matches.
//
// If no custom label selector is configured in the monitoring resource, the label selector check is
// `dash0.com/enable!=false`, that is, all workloads that
// - do not have the dash0.com/enable label, or,
// - that have that label with a value != false
// will be instrumentated.
func shouldBeInstrumented(objectMeta *metav1.ObjectMeta, autoInstrumentationLabelSelector string) bool {
	labelSelector, err := labels.Parse(autoInstrumentationLabelSelector)
	if err != nil {
		// The setting is validated in the validation webhook for the monitoring resource, so this should never happen.
		labelSelector, err = labels.Parse(dash0common.DefaultAutoInstrumentationLabelSelector)
		if err != nil {
			// This also should never, ever happen.
			panic(err)
		}
	}
	return labelSelector.Matches(labels.Set(objectMeta.Labels))
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

// ImageRefToLabel takes an image ref as input and returns a string that conforms to the spec of k8s labels:
// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
// If the image ref is longer than 63 characters, the registry part is dropped and the remaining part is truncated
// to 63 characters and it is ensured that the string ends with an alphanumeric character.
// Characters not supported in k8s labels are converted to underscores.
func ImageRefToLabel(imageRef string) string {
	if imageRef == "" {
		return imageRef
	}

	const maxLen = 63
	if len(imageRef) > maxLen {
		lastSlash := strings.LastIndex(imageRef, "/")
		if lastSlash != -1 {
			imageRef = imageRef[lastSlash+1:]
		}
	}
	if len(imageRef) > maxLen {
		imageRef = imageRef[:maxLen]
	}

	regex := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	imageRef = regex.ReplaceAllString(imageRef, "_")

	imageRef = strings.TrimRight(imageRef, "-._")

	return imageRef
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

// MergeMaps returns a new map combining the default labels/annotations managed by the operator with user-provided
// additional custom labels or annotations. The defaults take precedence, so that custom labels/annotations cannot
// override defaults (for example the labels used by selectors). The result is always a freshly allocated map (or nil
// when both inputs are empty); so follow-up mutations in downstream code (for example addCommonMetadata) do not
// leak back into one of the input maps.
func MergeMaps(defaults map[string]string, additional map[string]string) map[string]string {
	if len(additional) == 0 && len(defaults) == 0 {
		return nil
	}
	merged := make(map[string]string, len(defaults)+len(additional))
	maps.Copy(merged, additional)
	maps.Copy(merged, defaults)
	return merged
}
