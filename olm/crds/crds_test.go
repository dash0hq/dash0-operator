// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The CRD templates of the Helm chart are copies of the CRDs that controller-gen generates into config/crd/bases, and
// the OLM bundle takes its CRDs from the rendered chart.
var _ = Describe("The CRDs of the Helm chart", func() {
	It("match the CRDs in config/crd/bases", func() {
		generated := readGeneratedCrds()
		rendered := renderChartCrds()
		Expect(slices.Collect(maps.Keys(rendered))).To(ConsistOf(slices.Collect(maps.Keys(generated))))

		var outdated []string
		for name, generatedCrd := range generated {
			renderedCrd := rendered[name]
			// The chart adds labels, annotations and the conversion webhook.
			for _, field := range []string{"group", "names", "scope", "versions"} {
				if !reflect.DeepEqual(generatedCrd.Object["spec"].(map[string]any)[field],
					renderedCrd.Object["spec"].(map[string]any)[field]) {
					outdated = append(outdated, name+": spec."+field)
				}
			}
		}
		Expect(outdated).To(BeEmpty(),
			"copy the CRDs from config/crd/bases to the CRD templates of the Helm chart, then run make olm-bundle")
	})
})

func readGeneratedCrds() map[string]*unstructured.Unstructured {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(files).NotTo(BeEmpty())
	crds := make(map[string]*unstructured.Unstructured, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		Expect(err).NotTo(HaveOccurred())
		crd := &unstructured.Unstructured{}
		Expect(yaml.Unmarshal(content, &crd.Object)).To(Succeed(), file)
		crds[crd.GetName()] = crd
	}
	return crds
}

func renderChartCrds() map[string]*unstructured.Unstructured {
	// Some CRDs are only rendered with Signal Control enabled.
	cmd := exec.Command(
		"helm", "template", "dash0-operator", filepath.Join("..", "..", "helm-chart", "dash0-operator"),
		"--set", "operator.signalControl.enabled=true",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred(), stderr.String())
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	crds := make(map[string]*unstructured.Unstructured)
	for {
		object := &unstructured.Unstructured{}
		if err = decoder.Decode(&object.Object); errors.Is(err, io.EOF) {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if object.GetKind() == "CustomResourceDefinition" {
			crds[object.GetName()] = object
		}
	}
	return crds
}
