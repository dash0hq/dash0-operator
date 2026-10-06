// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

var _ = Describe("Converting a Dash0NotificationChannel to the API client's NotificationChannelDefinition", func() {
	It("loses no field of the notification channel spec", func() {
		notificationChannel := fullyPopulatedNotificationChannel()
		unstructuredNotificationChannel, err := structToMap(notificationChannel)
		Expect(err).ToNot(HaveOccurred())
		cleanUpMetadata(unstructuredNotificationChannel.Object)

		notificationChannelDefinition, err := mapToNotificationChannelDefinition(unstructuredNotificationChannel.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(notificationChannelDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		// mapToNotificationChannelDefinition transforms the map in place (display name moved to metadata.name,
		// emailV2Config moved to config), so the expected spec is taken after the conversion.
		expectedSpec := unstructuredNotificationChannel.Object["spec"].(map[string]any)
		Expect(expectedSpec).To(HaveKey("config"))
		Expect(expectedSpec).ToNot(HaveKey("display"))
		Expect(roundTripped["kind"]).To(Equal("Dash0NotificationChannel"))
		Expect(roundTripped["spec"]).To(Equal(expectedSpec))
		// Kubernetes labels and annotations are dropped, only the display name remains; the Dash0 API accepts the
		// resulting empty labels and annotations objects.
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name":        "Full Notification Channel",
			"labels":      map[string]any{},
			"annotations": map[string]any{},
		}))
	})
})

func fullyPopulatedNotificationChannel() *dash0v1beta1.Dash0NotificationChannel {
	plaintext := false
	return &dash0v1beta1.Dash0NotificationChannel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1beta1",
			Kind:       "Dash0NotificationChannel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-notification-channel",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "dropped",
			},
			Annotations: map[string]string{
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1beta1.Dash0NotificationChannelSpec{
			Display: dash0v1beta1.Dash0NotificationChannelDisplay{
				Name: "Full Notification Channel",
			},
			Type: "email_v2",
			EmailV2Config: &dash0v1beta1.EmailV2Config{
				Recipients: []string{"alerts@example.com", "oncall@example.com"},
				Plaintext:  &plaintext,
			},
			Frequency: "10m",
			Routing: &dash0v1beta1.Dash0NotificationChannelRouting{
				Assets: []dash0v1beta1.Dash0NotificationChannelRoutingAsset{
					{Kind: "check_rule", ID: "chk_01abc", Name: "High error rate", Dataset: "default"},
				},
				Filters: [][]dash0v1beta1.Dash0NotificationChannelRoutingFilter{
					{
						{Key: "k8s.namespace.name", Operator: "is", Value: "checkout"},
						{Key: "dash0.check.severity", Operator: "is_one_of", Values: []string{"critical", "degraded"}},
					},
					{
						{Key: "service.name", Operator: "is_set"},
					},
				},
			},
		},
	}
}
