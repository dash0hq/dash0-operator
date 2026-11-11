// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// annotationSizeSafetyMargin makes the test fail before a CRD reaches the limit, and leaves room for what the Helm
// chart and the deploy tools add to the generated CRDs: labels, the conversion webhook of Dash0Monitoring, annotations
// from Helm values (cert-manager) and from Argo CD (tracking id).
const annotationSizeSafetyMargin = 12 * 1024

// TestCrdsFitIntoTheLastAppliedConfigurationAnnotation guards against CRDs that a client-side kubectl apply (which Argo
// CD also uses by default) cannot install: kubectl stores the whole object in the last-applied-configuration
// annotation, and the API server rejects objects whose annotations exceed TotalAnnotationSizeLimitB in total.
func TestCrdsFitIntoTheLastAppliedConfigurationAnnotation(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
	if err != nil {
		t.Fatalf("cannot list the CRDs: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no CRDs found in config/crd/bases")
	}

	limit := apivalidation.TotalAnnotationSizeLimitB - annotationSizeSafetyMargin
	var tooLarge []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("cannot read %s: %v", file, err)
		}
		crd := &unstructured.Unstructured{}
		if err = yaml.Unmarshal(content, &crd.Object); err != nil {
			t.Fatalf("cannot parse %s: %v", file, err)
		}
		if size := annotationsSizeAfterClientSideApply(t, crd); size > limit {
			tooLarge = append(tooLarge, crd.GetName()+": "+strconv.Itoa(size)+" bytes")
		}
	}

	if len(tooLarge) > 0 {
		sort.Strings(tooLarge)
		t.Errorf(
			"the annotations of these CRDs exceed %d bytes (the API server limit of %d bytes minus a safety margin) after "+
				"a client-side kubectl apply:\n  %s\n\nShorten the Go doc comments in api/operator that controller-gen "+
				"renders into their descriptions.",
			limit,
			apivalidation.TotalAnnotationSizeLimitB,
			strings.Join(tooLarge, "\n  "),
		)
	}
}

// annotationsSizeAfterClientSideApply returns the total size of the annotations of the given CRD after a client-side
// kubectl apply has stored the CRD in the last-applied-configuration annotation. The serialization mirrors
// GetModifiedConfiguration in k8s.io/kubectl/pkg/util.
func annotationsSizeAfterClientSideApply(t *testing.T, crd *unstructured.Unstructured) int {
	t.Helper()
	annotations := crd.GetAnnotations()
	delete(annotations, corev1.LastAppliedConfigAnnotation)
	withoutLastApplied := crd.DeepCopy()
	withoutLastApplied.SetAnnotations(annotations)
	lastApplied, err := runtime.Encode(unstructured.UnstructuredJSONScheme, withoutLastApplied)
	if err != nil {
		t.Fatalf("cannot encode %s: %v", crd.GetName(), err)
	}

	size := len(corev1.LastAppliedConfigAnnotation) + len(lastApplied)
	for key, value := range annotations {
		size += len(key) + len(value)
	}
	return size
}
