// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The Perses dashboard conversion webhook", func() {

	It("converts v1alpha1 to v1alpha2 by wrapping spec contents in spec.config", func() {
		input := map[string]any{
			"apiVersion": "perses.dev/v1alpha1",
			"kind":       "PersesDashboard",
			"metadata": map[string]any{
				"name":      "example",
				"namespace": "default",
			},
			"spec": map[string]any{
				"duration": "30m",
				"display":  map[string]any{"name": "Example"},
				// Intentionally include a Dash0-specific variable kind that the typed conversion webhook from the Perses operator would
				// reject.
				"variables": []any{
					map[string]any{
						"kind": "Dash0FilterVariable",
						"spec": map[string]any{"name": "cloud_region"},
					},
				},
			},
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-1",
			DesiredAPIVersion: "perses.dev/v1alpha2",
			Objects:           []runtime.RawExtension{toRawObject(input)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		Expect(resp.Response.ConvertedObjects).To(HaveLen(1))
		out := parseConverted(resp.Response.ConvertedObjects[0])

		Expect(out["apiVersion"]).To(Equal("perses.dev/v1alpha2"))
		Expect(out["kind"]).To(Equal("PersesDashboard"))
		// metadata is untouched
		Expect(out["metadata"]).To(Equal(input["metadata"]))
		// spec is wrapped
		spec, ok := out["spec"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(spec).To(HaveKey("config"))
		config, ok := spec["config"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(config["duration"]).To(Equal("30m"))
		Expect(config["display"]).To(Equal(map[string]any{"name": "Example"}))
		variables, ok := config["variables"].([]any)
		Expect(ok).To(BeTrue())
		Expect(variables).To(HaveLen(1))
		variable, ok := variables[0].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(variable["kind"]).To(Equal("Dash0FilterVariable"))
	})

	It("converts v1alpha2 to v1alpha1 by unwrapping spec.config", func() {
		input := map[string]any{
			"apiVersion": "perses.dev/v1alpha2",
			"kind":       "PersesDashboard",
			"metadata":   map[string]any{"name": "example"},
			"spec": map[string]any{
				"config": map[string]any{
					"duration": "30m",
					"display":  map[string]any{"name": "Example"},
				},
			},
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-2",
			DesiredAPIVersion: "perses.dev/v1alpha1",
			Objects:           []runtime.RawExtension{toRawObject(input)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		out := parseConverted(resp.Response.ConvertedObjects[0])

		Expect(out["apiVersion"]).To(Equal("perses.dev/v1alpha1"))
		spec, ok := out["spec"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(spec).NotTo(HaveKey("config"))
		Expect(spec["duration"]).To(Equal("30m"))
	})

	It("passes through objects whose apiVersion already matches the desired version", func() {
		input := map[string]any{
			"apiVersion": "perses.dev/v1alpha2",
			"kind":       "PersesDashboard",
			"spec":       map[string]any{"config": map[string]any{"duration": "30m"}},
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-3",
			DesiredAPIVersion: "perses.dev/v1alpha2",
			Objects:           []runtime.RawExtension{toRawObject(input)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		out := parseConverted(resp.Response.ConvertedObjects[0])
		Expect(out).To(Equal(input))
	})

	It("handles missing spec by emitting spec.config={} when going v1alpha1 -> v1alpha2", func() {
		input := map[string]any{
			"apiVersion": "perses.dev/v1alpha1",
			"kind":       "PersesDashboard",
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-4",
			DesiredAPIVersion: "perses.dev/v1alpha2",
			Objects:           []runtime.RawExtension{toRawObject(input)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		out := parseConverted(resp.Response.ConvertedObjects[0])
		Expect(out["apiVersion"]).To(Equal("perses.dev/v1alpha2"))
		spec, ok := out["spec"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(spec["config"]).To(Equal(map[string]any{}))
	})

	It("returns a failure status on unsupported conversion directions", func() {
		input := map[string]any{
			"apiVersion": "perses.dev/v1alpha9",
			"kind":       "PersesDashboard",
			"spec":       map[string]any{},
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-5",
			DesiredAPIVersion: "perses.dev/v1alpha2",
			Objects:           []runtime.RawExtension{toRawObject(input)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusFailure))
		Expect(resp.Response.Result.Message).To(ContainSubstring("unsupported PersesDashboard conversion"))
		Expect(resp.Response.ConvertedObjects).To(BeEmpty())
	})

	It("converts multiple objects in a single request", func() {
		obj1 := map[string]any{
			"apiVersion": "perses.dev/v1alpha1",
			"kind":       "PersesDashboard",
			"spec":       map[string]any{"duration": "5m"},
		}
		obj2 := map[string]any{
			"apiVersion": "perses.dev/v1alpha1",
			"kind":       "PersesDashboard",
			"spec":       map[string]any{"duration": "10m"},
		}

		resp := postPersesDashboardCrdConversionReview(apiextensionsv1.ConversionRequest{
			UID:               "test-uid-6",
			DesiredAPIVersion: "perses.dev/v1alpha2",
			Objects:           []runtime.RawExtension{toRawObject(obj1), toRawObject(obj2)},
		})

		Expect(resp.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		Expect(resp.Response.ConvertedObjects).To(HaveLen(2))
		out1 := parseConverted(resp.Response.ConvertedObjects[0])
		out2 := parseConverted(resp.Response.ConvertedObjects[1])
		Expect(out1["spec"].(map[string]any)["config"].(map[string]any)["duration"]).To(Equal("5m"))
		Expect(out2["spec"].(map[string]any)["config"].(map[string]any)["duration"]).To(Equal("10m"))
	})

	It("rejects non-POST requests", func() {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, util.PersesDashboardConversionWebhookPath, strings.NewReader(""))
		servePersesDashboardConversion(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusMethodNotAllowed))
	})

	It("rejects requests with missing review.request", func() {
		body, err := json.Marshal(apiextensionsv1.ConversionReview{
			TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "ConversionReview"},
		})
		Expect(err).NotTo(HaveOccurred())

		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, util.PersesDashboardConversionWebhookPath, bytes.NewReader(body))
		servePersesDashboardConversion(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		_, _ = io.ReadAll(recorder.Body)
	})
})

func postPersesDashboardCrdConversionReview(req apiextensionsv1.ConversionRequest) apiextensionsv1.ConversionReview {
	review := apiextensionsv1.ConversionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "ConversionReview",
		},
		Request: &req,
	}
	body, err := json.Marshal(&review)
	Expect(err).NotTo(HaveOccurred())

	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, util.PersesDashboardConversionWebhookPath, bytes.NewReader(body))
	servePersesDashboardConversion(recorder, httpReq)

	Expect(recorder.Code).To(Equal(http.StatusOK))
	var resp apiextensionsv1.ConversionReview
	Expect(json.NewDecoder(recorder.Body).Decode(&resp)).To(Succeed())
	Expect(resp.Response).NotTo(BeNil())
	Expect(resp.Response.UID).To(Equal(req.UID))
	return resp
}

func toRawObject(obj map[string]any) runtime.RawExtension {
	raw, err := json.Marshal(obj)
	Expect(err).NotTo(HaveOccurred())
	return runtime.RawExtension{Raw: raw}
}

func parseConverted(raw runtime.RawExtension) map[string]any {
	out := map[string]any{}
	Expect(json.Unmarshal(raw.Raw, &out)).To(Succeed())
	return out
}
