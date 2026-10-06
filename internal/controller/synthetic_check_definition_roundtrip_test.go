// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

var _ = Describe("Converting a Dash0SyntheticCheck to the API client's SyntheticCheckDefinition", func() {
	It("loses no field of the synthetic check spec", func() {
		syntheticCheck := fullyPopulatedSyntheticCheck()
		unstructuredSyntheticCheck, err := structToMap(syntheticCheck)
		Expect(err).ToNot(HaveOccurred())
		cleanUpMetadata(unstructuredSyntheticCheck.Object)

		syntheticCheckDefinition, err := mapToSyntheticCheckDefinition(unstructuredSyntheticCheck.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(syntheticCheckDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["kind"]).To(Equal("Dash0SyntheticCheck"))
		Expect(roundTripped["spec"]).To(Equal(unstructuredSyntheticCheck.Object["spec"]))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-synthetic-check",
			"annotations": map[string]any{
				"dash0.com/folder-path": "/shop/checkout",
				"dash0.com/sharing":     "team:team_01abc",
			},
		}))
	})
})

func fullyPopulatedSyntheticCheck() *dash0v1alpha1.Dash0SyntheticCheck {
	return &dash0v1alpha1.Dash0SyntheticCheck{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SyntheticCheck",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-synthetic-check",
			Namespace: "namespace",
			Annotations: map[string]string{
				"dash0.com/folder-path":           "/shop/checkout",
				"dash0.com/sharing":               "team:team_01abc",
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0SyntheticCheckSpec{
			Display: dash0v1alpha1.Dash0SyntheticCheckDisplay{
				Name: "Full Synthetic Check",
			},
			Plugin: dash0v1alpha1.Dash0SyntheticCheckPlugin{
				Kind: "http",
				Spec: dash0v1alpha1.Dash0SyntheticCheckHTTPPluginSpec{
					Request: dash0v1alpha1.Dash0SyntheticCheckHTTPRequest{
						Method:    "post",
						URL:       "https://shop.example.com/api/checkout",
						Redirects: "follow",
						TLS:       dash0v1alpha1.Dash0SyntheticCheckHTTPTLS{AllowInsecure: true},
						Tracing:   dash0v1alpha1.Dash0SyntheticCheckHTTPTracing{AddTracingHeaders: true},
						Headers: []dash0v1alpha1.Dash0SyntheticCheckHTTPHeader{
							{Name: "Accept", Value: "application/json"},
						},
						QueryParameters: []dash0v1alpha1.Dash0SyntheticCheckHTTPQueryParameter{
							{Name: "dryRun", Value: "true"},
						},
						BasicAuthentication: &dash0v1alpha1.Dash0SyntheticCheckHTTPBasicAuthentication{
							Username: "user",
							Password: "password",
						},
						Body: &dash0v1alpha1.Dash0SyntheticCheckHTTPBody{
							Kind: "json",
							Spec: dash0v1alpha1.Dash0SyntheticCheckHTTPBodySpec{Content: `{"items":[]}`},
						},
					},
					Assertions: dash0v1alpha1.Dash0SyntheticCheckHTTPAssertions{
						CriticalAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{
							{
								Kind: "status_code",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("is"),
									Value:    ptr.To("200"),
								},
							},
							{
								Kind: "json_body",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("is"),
									Value:    ptr.To("ok"),
									JSONPath: ptr.To("$.status"),
								},
							},
						},
						DegradedAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{
							{
								Kind: "response_header",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("is"),
									Key:      ptr.To("Content-Type"),
									Value:    ptr.To("application/json"),
								},
							},
							{
								Kind: "timing",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("lte"),
									Type:     ptr.To("total"),
									Value:    ptr.To("500ms"),
								},
							},
						},
					},
				},
			},
			Schedule: dash0v1alpha1.Dash0SyntheticCheckSchedule{
				Strategy:  "all_locations",
				Interval:  "1m",
				Locations: []string{"de-frankfurt", "us-oregon"},
			},
			Retries: dash0v1alpha1.Dash0SyntheticCheckRetries{
				Kind: "exponential",
				Spec: dash0v1alpha1.Dash0SyntheticCheckRetriesSpec{
					Attempts:     ptr.To(3),
					Delay:        ptr.To("1s"),
					MaximumDelay: ptr.To("10s"),
				},
			},
			Notifications: dash0v1alpha1.Dash0SyntheticCheckNotifications{
				Channels: []dash0v1alpha1.NotificationChannelID{
					"0f3c6a8e-1d2b-4c5d-8e9f-0a1b2c3d4e5f",
					"7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d",
				},
			},
			Enabled: true,
			Labels: map[string]string{
				"team": "checkout",
			},
		},
	}
}
