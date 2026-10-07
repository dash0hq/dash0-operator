// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
)

var _ = Describe("Converting a Dash0Team to the API client's TeamDefinition", func() {
	It("loses no field of the team spec", func() {
		unstructuredTeam, err := structToMap(fullyPopulatedTeam())
		Expect(err).ToNot(HaveOccurred())

		teamDefinition, err := mapToTeamDefinition(unstructuredTeam.Object, "full-team")
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(teamDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["apiVersion"]).To(Equal("dash0.com/v1alpha1"))
		Expect(roundTripped["kind"]).To(Equal("Dash0Team"))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"display": map[string]any{
				"name":        "Backend Team",
				"description": "Owns backend services.",
				"color": map[string]any{
					"from": "#6366F1",
					"to":   "#8B5CF6",
				},
			},
			"members": []any{"alice@example.com", "user_01ABC"},
		}))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-team",
			"labels": map[string]any{
				"app.kubernetes.io/name": "ignored-by-the-api",
			},
			"annotations": map[string]any{
				"example.com/not-part-of-the-api": "ignored-by-the-api",
			},
		}))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})
})

func fullyPopulatedTeam() *dash0dashv1alpha1.Dash0Team {
	return &dash0dashv1alpha1.Dash0Team{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dash0.com/v1alpha1",
			Kind:       "Dash0Team",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-team",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ignored-by-the-api",
			},
			Annotations: map[string]string{
				"example.com/not-part-of-the-api": "ignored-by-the-api",
			},
		},
		Spec: dash0dashv1alpha1.Dash0TeamSpec{
			Display: dash0dashv1alpha1.Dash0TeamDisplay{
				Name:        "Backend Team",
				Description: "Owns backend services.",
				Color: dash0dashv1alpha1.Dash0TeamColor{
					From: "#6366F1",
					To:   "#8B5CF6",
				},
			},
			Members: []string{"alice@example.com", "user_01ABC"},
		},
		Status: dash0dashv1alpha1.Dash0TeamStatus{
			SynchronizationStatus: "successful",
		},
	}
}
