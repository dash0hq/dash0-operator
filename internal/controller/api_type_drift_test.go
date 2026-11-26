// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var jsonMarshalerType = reflect.TypeFor[json.Marshaler]()

/*
apiTypeDrift compares the JSON field paths of a custom resource type with the corresponding type of the Dash0 API
client, which is generated from the Dash0 OpenAPI spec. It returns the paths that only exist in one of the two types.
Nested paths are only reported if their parent path exists in both types, e.g. a struct that is missing entirely on
one side is reported once, not once per field.
*/
func apiTypeDrift(crdType reflect.Type, clientType reflect.Type) ([]string, []string) {
	crdPaths := jsonFieldPaths(crdType)
	clientPaths := jsonFieldPaths(clientType)
	return topmostMissingPaths(crdPaths, clientPaths), topmostMissingPaths(clientPaths, crdPaths)
}

func topmostMissingPaths(paths map[string]bool, otherPaths map[string]bool) []string {
	var missing []string
	for path := range paths {
		if otherPaths[path] {
			continue
		}
		if parent, hasParent := parentPath(path); hasParent && !otherPaths[parent] {
			continue
		}
		missing = append(missing, path)
	}
	slices.Sort(missing)
	return missing
}

func parentPath(path string) (string, bool) {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return "", false
	}
	return path[:idx], true
}

func jsonFieldPaths(t reflect.Type) map[string]bool {
	paths := map[string]bool{}
	collectJsonFieldPaths(t, "", paths, map[reflect.Type]bool{})
	return paths
}

func collectJsonFieldPaths(t reflect.Type, prefix string, paths map[string]bool, visiting map[reflect.Type]bool) {
	t = elementType(t)
	if !isJsonObject(t) || visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		name, inline := jsonFieldName(field)
		if name == "-" {
			continue
		}
		if inline {
			collectJsonFieldPaths(field.Type, prefix, paths, visiting)
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		paths[path] = true
		collectJsonFieldPaths(field.Type, path, paths, visiting)
	}
}

func elementType(t reflect.Type) reflect.Type {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return t
		}
	}
}

func isJsonObject(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	if t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType) {
		return false
	}
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "-", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name, field.Anonymous
	}
	return name, false
}

/*
expectNoUnexpectedApiTypeDrift fails if the custom resource type and the Dash0 API client type differ in a field path
that is not listed in the allow-lists, or if an allow-list entry is no longer a difference. The allow-lists map a path
to the reason why the difference is accepted.
*/
func expectNoUnexpectedApiTypeDrift(
	crdType reflect.Type,
	clientType reflect.Type,
	allowedOnlyInCrd map[string]string,
	allowedOnlyInClient map[string]string,
) {
	GinkgoHelper()
	onlyInCrd, onlyInClient := apiTypeDrift(crdType, clientType)
	Expect(onlyInCrd).To(ConsistOf(mapKeys(allowedOnlyInCrd)),
		"fields of %s that do not exist in %s; they are not synchronized to the Dash0 API",
		crdType, clientType)
	Expect(onlyInClient).To(ConsistOf(mapKeys(allowedOnlyInClient)),
		"fields of %s that do not exist in %s; the Dash0 API supports them, the custom resource does not",
		clientType, crdType)
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

type driftTestUnion struct {
	union json.RawMessage //nolint:unused
}

type driftTestNested struct {
	Key   string           `json:"key"`
	Value driftTestUnion   `json:"value,omitempty"`
	Time  metav1.Time      `json:"time"`
	Self  *driftTestNested `json:"self,omitempty"`
}

type driftTestInline struct {
	Inlined string `json:"inlined"`
}

type driftTestCrd struct {
	driftTestInline `json:",inline"`
	Name            string            `json:"name"`
	Ignored         string            `json:"-"`
	Items           []driftTestNested `json:"items"`
	OnlyCrd         *driftTestNested  `json:"onlyCrd,omitempty"`
}

type driftTestClient struct {
	Inlined    string                     `json:"inlined"`
	Name       *string                    `json:"name,omitempty"`
	Items      *[]driftTestNested         `json:"items,omitempty"`
	Labels     map[string]driftTestNested `json:"labels"`
	OnlyClient string                     `json:"onlyClient"`
}

var _ = Describe("Comparing custom resource types with Dash0 API client types", func() {
	It("collects JSON field paths through pointers, slices, maps, inline fields and recursive types", func() {
		Expect(jsonFieldPaths(reflect.TypeFor[driftTestCrd]())).To(Equal(map[string]bool{
			"inlined":       true,
			"name":          true,
			"items":         true,
			"items.key":     true,
			"items.value":   true,
			"items.time":    true,
			"items.self":    true,
			"onlyCrd":       true,
			"onlyCrd.key":   true,
			"onlyCrd.value": true,
			"onlyCrd.time":  true,
			"onlyCrd.self":  true,
		}))
	})

	It("reports only the topmost path that is missing on the other side", func() {
		onlyInCrd, onlyInClient := apiTypeDrift(reflect.TypeFor[driftTestCrd](), reflect.TypeFor[driftTestClient]())
		Expect(onlyInCrd).To(Equal([]string{"onlyCrd"}))
		Expect(onlyInClient).To(Equal([]string{"labels", "onlyClient"}))
	})

	It("accepts allow-listed differences", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[driftTestCrd](),
			reflect.TypeFor[driftTestClient](),
			map[string]string{"onlyCrd": "test"},
			map[string]string{"labels": "test", "onlyClient": "test"},
		)
	})

	It("fails for an allow-list entry that is no longer a difference", func() {
		failures := InterceptGomegaFailures(func() {
			expectNoUnexpectedApiTypeDrift(
				reflect.TypeFor[driftTestCrd](),
				reflect.TypeFor[driftTestClient](),
				map[string]string{"onlyCrd": "test", "name": "stale"},
				map[string]string{"labels": "test", "onlyClient": "test"},
			)
		})
		Expect(failures).To(HaveLen(1))
	})

	DescribeTable("finds no unexpected drift between the custom resource definitions and the Dash0 API client types",
		func(check crdSchemaDriftCheck) {
			expectNoUnexpectedCrdSchemaDrift(check)
		},
		crdSchemaDriftCheckEntries(),
	)

	It("compares every variant of the Dash0 API client unions with a custom resource definition", func() {
		uncovered := uncoveredUnionVariants(crdSchemaDriftChecks)
		var unexpected []string
		for _, variant := range uncovered {
			if _, allowed := unsupportedApiClientUnionVariants[variant]; !allowed {
				unexpected = append(unexpected, variant.String())
			}
		}
		var stale []string
		for variant := range unsupportedApiClientUnionVariants {
			if !slices.Contains(uncovered, variant) {
				stale = append(stale, variant.String())
			}
		}
		Expect(unexpected).To(BeEmpty(),
			"union variants of the Dash0 API client that no drift check compares with a custom resource definition")
		Expect(stale).To(BeEmpty(),
			"entries in unsupportedApiClientUnionVariants that are compared by a drift check (or no longer exist)")
	})

	It("finds no unexpected drift between CheckRule and the API client's check rule type", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[CheckRule](),
			reflect.TypeFor[dash0apiclient.PrometheusAlertRule](),
			nil,
			map[string]string{
				"dataset":    "sent as the dataset query parameter",
				"enabled":    "not supported by PrometheusRule",
				"id":         "check rules are addressed by their origin in the URL path",
				"metadata":   "server-populated and read-only",
				"thresholds": "sent as the dash0-threshold-degraded/-critical annotations",
			},
		)
	})

	It("finds no unexpected drift between PrometheusRule and the API client's recording rule type", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[prometheusv1.PrometheusRuleSpec](),
			reflect.TypeFor[dash0apiclient.PrometheusRuleSpec](),
			map[string]string{
				"groups.limit":                     "not supported by the Dash0 API",
				"groups.partial_response_strategy": "not supported by the Dash0 API",
			},
			nil,
		)
	})
})
