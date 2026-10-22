// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package samples

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
		owned := readOwnedCrds()
		storageVersions := readStorageVersions()

		ownedTypes := make([]string, 0, len(owned))
		for _, crd := range owned {
			name := crd["name"].(string)
			version := crd["version"].(string)
			Expect(version).To(Equal(storageVersions[name]), name)
			ownedTypes = append(ownedTypes, ownedType(crd))
		}
		samples := readSamples()
		sampleTypes := make([]string, 0, len(samples))
		for _, sample := range samples {
			sampleTypes = append(sampleTypes, fmt.Sprintf("%s %s", sample.GetAPIVersion(), sample.GetKind()))
		}
		Expect(sampleTypes).To(ConsistOf(ownedTypes))
	})

	// Mirrors the scorecard test olm-spec-descriptors, which needs a cluster.
	It("have a spec descriptor in the CSV base for each top-level spec field", func() {
		descriptorPaths := make(map[string][]string)
		for _, crd := range readOwnedCrds() {
			for _, descriptor := range specDescriptors(crd) {
				descriptorPaths[ownedType(crd)] = append(descriptorPaths[ownedType(crd)], descriptor["path"].(string))
			}
		}
		var failures []string
		for _, sample := range readSamples() {
			sampleType := fmt.Sprintf("%s %s", sample.GetAPIVersion(), sample.GetKind())
			spec, found, err := unstructured.NestedMap(sample.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue(), sampleType)
			for field := range spec {
				if !slices.Contains(descriptorPaths[sampleType], field) {
					failures = append(failures, fmt.Sprintf("%s: spec.%s", sampleType, field))
				}
			}
		}
		Expect(failures).To(BeEmpty())
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

// The console skips descriptors whose path does not exist in the CRD schema and only logs them to the browser console.
var _ = Describe("The spec descriptors of the CSV base", func() {
	It("refer to fields of the CRD schemas", func() {
		specSchemas := readStorageVersionSpecSchemas()
		var failures []string
		for _, crd := range readOwnedCrds() {
			for _, descriptor := range specDescriptors(crd) {
				path := descriptor["path"].(string)
				if !schemaHasPath(specSchemas[crd["name"].(string)], path) {
					failures = append(failures, fmt.Sprintf("%s: spec.%s", ownedType(crd), path))
				}
			}
		}
		Expect(failures).To(BeEmpty())
	})
})

func readOwnedCrds() []map[string]any {
	csvBase := readYaml(filepath.Join("..", "..", "config", "manifests", "bases",
		"dash0-operator.clusterserviceversion.yaml"))
	owned, found, err := unstructured.NestedSlice(csvBase.Object, "spec", "customresourcedefinitions", "owned")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	crds := make([]map[string]any, 0, len(owned))
	for _, entry := range owned {
		crds = append(crds, entry.(map[string]any))
	}
	return crds
}

// ownedType returns "<group>/<version> <kind>" for an owned CRD of the CSV base, the format matches the apiVersion and
// kind of a sample.
func ownedType(crd map[string]any) string {
	_, group, _ := strings.Cut(crd["name"].(string), ".")
	return fmt.Sprintf("%s/%s %s", group, crd["version"], crd["kind"])
}

func specDescriptors(crd map[string]any) []map[string]any {
	entries, _ := crd["specDescriptors"].([]any)
	descriptors := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		descriptors = append(descriptors, entry.(map[string]any))
	}
	return descriptors
}

// schemaHasPath checks a descriptor path like "exports[0].dash0.authorization" against the schema of a CRD's spec.
func schemaHasPath(schema map[string]any, path string) bool {
	for segment := range strings.SplitSeq(path, ".") {
		name, _, _ := strings.Cut(segment, "[")
		properties, _ := schema["properties"].(map[string]any)
		var ok bool
		if schema, ok = properties[name].(map[string]any); !ok {
			return false
		}
		for range strings.Count(segment, "[") {
			if schema, ok = schema["items"].(map[string]any); !ok {
				return false
			}
		}
	}
	return true
}

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
	storageVersions := make(map[string]string)
	for name, version := range readStorageVersionsOfCrds() {
		storageVersions[name] = version["name"].(string)
	}
	return storageVersions
}

func readStorageVersionSpecSchemas() map[string]map[string]any {
	specSchemas := make(map[string]map[string]any)
	for name, version := range readStorageVersionsOfCrds() {
		specSchema, found, err := unstructured.NestedMap(version, "schema", "openAPIV3Schema", "properties", "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), name)
		specSchemas[name] = specSchema
	}
	return specSchemas
}

// readStorageVersionsOfCrds returns the storage version entry of spec.versions for each CRD in config/crd/bases.
func readStorageVersionsOfCrds() map[string]map[string]any {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
	Expect(err).NotTo(HaveOccurred())
	storageVersions := make(map[string]map[string]any, len(files))
	for _, file := range files {
		crd := readYaml(file)
		versions, _, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
		Expect(err).NotTo(HaveOccurred())
		for _, entry := range versions {
			version := entry.(map[string]any)
			if version["storage"] == true {
				storageVersions[crd.GetName()] = version
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
