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

var _ = Describe("Converting a Dash0SpamFilter to the API client's SpamFilterDefinition", func() {
	It("loses no field of the spam filter spec", func() {
		unstructuredSpamFilter, err := structToMap(fullyPopulatedSpamFilter())
		Expect(err).ToNot(HaveOccurred())
		expectedSpec := unstructuredSpamFilter.Object["spec"]

		spamFilterDefinition, err := mapToSpamFilterDefinition(unstructuredSpamFilter.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(spamFilterDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["apiVersion"]).To(Equal("operator.dash0.com/v1alpha1"))
		Expect(roundTripped["kind"]).To(Equal("Dash0SpamFilter"))
		Expect(roundTripped["spec"]).To(Equal(expectedSpec))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-spam-filter",
			"annotations": map[string]any{
				"dash0.com/enabled": "false",
			},
			"labels": map[string]any{},
		}))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})
})

func fullyPopulatedSpamFilter() *dash0v1alpha1.Dash0SpamFilter {
	return &dash0v1alpha1.Dash0SpamFilter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SpamFilter",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-spam-filter",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "dropped",
			},
			Annotations: map[string]string{
				"dash0.com/enabled":               "false",
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0SpamFilterSpec{
			Contexts: []string{"log", "span", "metric"},
			Filter: []dash0v1alpha1.Dash0SpamFilterCondition{
				{
					Key:      "k8s.namespace.name",
					Operator: "is",
					Value:    ptr.To("kube-system"),
				},
				{
					Key:      "service.name",
					Operator: "is_one_of",
					Values:   []string{"health-checker", "load-generator"},
				},
				{
					Key:      "http.route",
					Operator: "is_set",
				},
			},
		},
		Status: dash0v1alpha1.Dash0SpamFilterStatus{
			SynchronizationStatus: "successful",
		},
	}
}
