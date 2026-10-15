// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

var _ = Describe("Converting a Dash0View to the API client's ViewDefinition", func() {
	It("loses no field of the view spec", func() {
		view := fullyPopulatedView()
		unstructuredView, err := structToMap(view)
		Expect(err).ToNot(HaveOccurred())
		cleanUpMetadata(unstructuredView.Object)

		viewDefinition, err := mapToViewDefinition(unstructuredView.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(viewDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		expectedSpec := unstructuredView.Object["spec"].(map[string]any)
		// The Dash0 API ignores spec.display.folder since folders moved to the dash0.com/folder-path annotation.
		delete(expectedSpec["display"].(map[string]any), "folder")
		Expect(roundTripped["kind"]).To(Equal("Dash0View"))
		Expect(roundTripped["spec"]).To(Equal(expectedSpec))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-view",
			"annotations": map[string]any{
				"dash0.com/folder-path": "/a/b",
				"dash0.com/sharing":     "team:team_01abc",
			},
		}))
	})
})

func fullyPopulatedView() *dash0v1alpha1.Dash0View {
	return &dash0v1alpha1.Dash0View{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0View",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-view",
			Namespace: "namespace",
			Annotations: map[string]string{
				"dash0.com/folder-path": "/a/b",
				"dash0.com/sharing":     "team:team_01abc",
			},
		},
		Spec: dash0v1alpha1.Dash0ViewSpec{
			Type: "spans",
			Display: dash0v1alpha1.Dash0ViewDisplay{
				Name:        "Full View",
				Description: "every field set",
				Folder:      []string{"a", "b"},
			},
			Permissions: []dash0v1alpha1.Dash0ViewPermission{
				{TeamId: "team_01abc", Actions: []dash0v1alpha1.Dash0ViewAction{"views:read"}},
				{UserId: "user_01abc", Actions: []dash0v1alpha1.Dash0ViewAction{"views:write"}},
				{Role: "admin", Actions: []dash0v1alpha1.Dash0ViewAction{"views:delete"}},
			},
			GroupBy: []string{"service.name", "k8s.namespace.name"},
			Filter: []dash0v1alpha1.Dash0ViewFilter{
				{Key: "service.name", Operator: "is", Value: "checkout"},
				{Key: "http.status_code", Operator: "is_one_of", Values: []string{"500", "503"}},
			},
			ImplicitFilter: []dash0v1alpha1.Dash0ViewFilter{
				{Key: "k8s.namespace.name", Operator: "is_set"},
			},
			Table: &dash0v1alpha1.Dash0ViewTable{
				Columns: []dash0v1alpha1.Dash0ViewTableColumn{
					{Key: "service.name", Label: "Service", ColSize: "minmax(auto, 2fr)"},
				},
				Sort: []dash0v1alpha1.Dash0ViewTableSort{
					{Key: "service.name", Direction: "ascending"},
				},
			},
			Visualizations: []dash0v1alpha1.Dash0ViewVisualization{
				{
					YAxisScale: "linear",
					Metric:     "dash0.spans",
					Renderer:   "dash0:spans-histogram",
					Renderers:  []dash0v1alpha1.Dash0ViewRenderer{"dash0:spans-histogram"},
				},
			},
		},
	}
}
