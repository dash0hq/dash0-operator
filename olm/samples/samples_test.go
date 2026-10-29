// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package samples

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The samples in config/samples become the alm-examples of the OLM bundle, which the OpenShift console uses as the
// defaults of its create forms.
var _ = Describe("The samples of the OLM bundle", func() {

	// The bundle only serves the storage version of CRDs with a conversion webhook, so the CSV base and the samples have
	// to use the storage version.
	It("have one sample for the storage version of each owned CRD of the CSV base", func() {
		csvBase := readYaml(filepath.Join("..", "..", "config", "manifests", "bases",
			"dash0-operator.clusterserviceversion.yaml"))
		owned, found, err := unstructured.NestedSlice(csvBase.Object, "spec", "customresourcedefinitions", "owned")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		storageVersions := readStorageVersions()

		ownedTypes := make([]string, 0, len(owned))
		for _, entry := range owned {
			crd := entry.(map[string]any)
			name := crd["name"].(string)
			version := crd["version"].(string)
			Expect(version).To(Equal(storageVersions[name]), name)
			_, group, _ := strings.Cut(name, ".")
			ownedTypes = append(ownedTypes, fmt.Sprintf("%s/%s %s", group, version, crd["kind"]))
		}
		samples := readSamples()
		sampleTypes := make([]string, 0, len(samples))
		for _, sample := range samples {
			sampleTypes = append(sampleTypes, fmt.Sprintf("%s %s", sample.GetAPIVersion(), sample.GetKind()))
		}
		Expect(sampleTypes).To(ConsistOf(ownedTypes))
	})

	It("are valid according to the CRD schemas", func() {
		ctx := context.Background()
		var failures []string
		for _, sample := range readSamples() {
			namespaced, err := k8sClient.IsObjectNamespaced(sample)
			Expect(err).NotTo(HaveOccurred())
			if namespaced {
				sample.SetNamespace("default")
			}
			if err = k8sClient.Create(
				ctx,
				sample,
				client.DryRunAll,
				client.FieldValidation("Strict"),
			); err != nil {
				failures = append(failures, fmt.Sprintf("%s/%s: %v", sample.GetKind(), sample.GetName(), err))
			}
		}
		Expect(failures).To(BeEmpty())
	})
})

func readSamples() []*unstructured.Unstructured {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "*.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(files).NotTo(BeEmpty())
	samples := make([]*unstructured.Unstructured, 0, len(files))
	for _, file := range files {
		samples = append(samples, readYaml(file))
	}
	return samples
}

func readStorageVersions() map[string]string {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
	Expect(err).NotTo(HaveOccurred())
	storageVersions := make(map[string]string, len(files))
	for _, file := range files {
		crd := readYaml(file)
		versions, _, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
		Expect(err).NotTo(HaveOccurred())
		for _, entry := range versions {
			version := entry.(map[string]any)
			if version["storage"] == true {
				storageVersions[crd.GetName()] = version["name"].(string)
			}
		}
	}
	return storageVersions
}

func readYaml(file string) *unstructured.Unstructured {
	content, err := os.ReadFile(file)
	Expect(err).NotTo(HaveOccurred())
	object := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(content, &object.Object)).To(Succeed(), file)
	return object
}
