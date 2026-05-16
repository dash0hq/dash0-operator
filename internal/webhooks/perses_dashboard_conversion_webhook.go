// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"
	"fmt"
	"net/http"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	persesGroupV1Alpha1 = "perses.dev/v1alpha1"
	persesGroupV1Alpha2 = "perses.dev/v1alpha2"
)

// SetupPersesDashboardConversionWebhookWithManager registers our hand-rolled JSON-level
// conversion handler for the PersesDashboard CRD on the manager's webhook server.
func SetupPersesDashboardConversionWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(
		util.PersesDashboardConversionWebhookPath,
		http.HandlerFunc(servePersesDashboardConversion),
	)
	return nil
}

func servePersesDashboardConversion(w http.ResponseWriter, r *http.Request) {
	logger := logd.NewLogger(ctrl.Log.WithName("perses-dashboard-conversion-webhook"))
	if r.Method != http.MethodPost {
		http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	var review apiextensionsv1.ConversionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		http.Error(w, fmt.Sprintf("cannot decode ConversionReview: %v", err), http.StatusBadRequest)
		return
	}
	if review.Request == nil {
		http.Error(w, "ConversionReview.request is nil", http.StatusBadRequest)
		return
	}

	response := &apiextensionsv1.ConversionResponse{UID: review.Request.UID}
	desired := review.Request.DesiredAPIVersion

	converted, err := convertPersesDashboardObjects(review.Request.Objects, desired)
	if err != nil {
		response.Result = metav1.Status{
			Status:  metav1.StatusFailure,
			Message: err.Error(),
		}
	} else {
		response.ConvertedObjects = converted
		response.Result = metav1.Status{Status: metav1.StatusSuccess}
	}

	review.Response = response
	review.Request = nil

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&review); err != nil {
		logger.Error(err, "failed to encode PersesDashboard ConversionReview response")
	}
}

func convertPersesDashboardObjects(
	objects []runtime.RawExtension,
	desiredAPIVersion string,
) ([]runtime.RawExtension, error) {
	out := make([]runtime.RawExtension, 0, len(objects))
	for i, raw := range objects {
		obj := map[string]any{}
		if err := json.Unmarshal(raw.Raw, &obj); err != nil {
			return nil, fmt.Errorf("cannot unmarshal converted object[%d]: %w", i, err)
		}
		converted, err := convertOnePersesDashboard(obj, desiredAPIVersion)
		if err != nil {
			return nil, fmt.Errorf("cannot convert object[%d]: %w", i, err)
		}
		serialized, err := json.Marshal(converted)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal converted object[%d]: %w", i, err)
		}
		out = append(out, runtime.RawExtension{Raw: serialized})
	}
	return out, nil
}

// convertOnePersesDashboard returns a copy of the given object converted to desiredAPIVersion. The only structural
// difference between v1alpha1 and v1alpha2 is that v1alpha2 wraps the dashboard spec in `spec.config`; everything
// inside is identical between the two versions.
func convertOnePersesDashboard(source map[string]any, desiredAPIVersion string) (map[string]any, error) {
	target := make(map[string]any, len(source))
	for k, v := range source {
		target[k] = v
	}

	sourceAPIVersion, _ := target["apiVersion"].(string)
	if sourceAPIVersion == desiredAPIVersion {
		return target, nil
	}

	spec, _ := target["spec"].(map[string]any)

	switch {
	case sourceAPIVersion == persesGroupV1Alpha1 && desiredAPIVersion == persesGroupV1Alpha2:
		// v1alpha1 → v1alpha2: nest everything under spec.config.
		if spec == nil {
			spec = map[string]any{}
		}
		target["spec"] = map[string]any{"config": spec}

	case sourceAPIVersion == persesGroupV1Alpha2 && desiredAPIVersion == persesGroupV1Alpha1:
		// v1alpha2 → v1alpha1: unwrap spec.config.
		if spec != nil {
			if config, ok := spec["config"].(map[string]any); ok {
				target["spec"] = config
			}
		}

	default:
		return nil, fmt.Errorf(
			"unsupported PersesDashboard conversion from %q to %q",
			sourceAPIVersion,
			desiredAPIVersion,
		)
	}

	target["apiVersion"] = desiredAPIVersion
	return target, nil
}
