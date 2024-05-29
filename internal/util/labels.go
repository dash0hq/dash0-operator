// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	InstrumentedLabelKey              = "dash0.instrumented"
	OperatorVersionLabelKey           = "dash0.operator.version"
	InitContainerImageVersionLabelKey = "dash0.initcontainer.image.version"
	InstrumentedByLabelKey            = "dash0.instrumented.by"
	WebhookIgnoreOnceLabelKey         = "dash0.webhook.ignore.once"
)

func AddWebhookIgnoreOnceLabel(meta *metav1.ObjectMeta) {
	AddLabel(meta, WebhookIgnoreOnceLabelKey, "true")
}

func AddLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}
	meta.Labels[key] = value
}
