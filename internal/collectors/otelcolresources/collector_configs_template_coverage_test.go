// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This is the drift protection for the collector configuration matrix in collector_configs_matrix_test.go: it collects
// every condition the collector configuration templates branch on and verifies that the matrix renders both sides of
// each of them. Without it, a new {{ if .SomeNewSetting }} in a template would be validated against the collector
// binary for one of its two branches only, which is precisely the gap that let OPE-568 reach a release.
//
// Conditions inside a range or with body are not collected: those rebind the dot, so their field paths do not refer to
// the template values. They are covered indirectly, because covering both sides of the enclosing condition means
// rendering the body at least once.

// conditionsWithUnreachableFalseBranch lists the template conditions whose false branch no configuration can reach,
// together with the reason. The test verifies that these conditions are indeed never false, so an entry that becomes
// reachable fails the test instead of silently excusing a gap in the matrix.
var conditionsWithUnreachableFalseBranch = map[string]string{
	"LabelAndAnnotationExclusionPatterns": "derived from the non-empty labelAndAnnotationKeysExcludedFromCollection",
	"NamespaceOttlFilter":                 "renderOttlNamespaceFilter always renders at least the k8s.namespace.name condition",
	// getDefaultOtlpExporters returns no exporters exactly when the operator configuration has no exports configured,
	// and CollectorManager#ReconcileOpenTelemetryCollector removes the collectors instead of rendering them in that
	// case. The default export pipelines are therefore never rendered without exporters.
	"Exporters.Default": "the collectors are not deployed at all when the operator configuration has no exports",
}

var _ = Describe("The collector configuration matrix", func() {

	It("should cover both sides of every condition in the collector configuration templates", func() {
		conditionPaths := collectTemplateConditionPaths()
		Expect(conditionPaths).ToNot(BeEmpty())

		truthyIn := map[string][]string{}
		falsyIn := map[string][]string{}
		for _, entry := range collectorConfigMatrix() {
			values := entry.templateValues()
			for _, path := range conditionPaths {
				truth, err := evaluateTemplateCondition(values, path)
				Expect(err).ToNot(
					HaveOccurred(),
					"cannot evaluate the template condition %s for the matrix entry %s",
					path,
					entry.name,
				)
				if truth {
					truthyIn[path] = append(truthyIn[path], entry.name)
				} else {
					falsyIn[path] = append(falsyIn[path], entry.name)
				}
			}
		}

		var uncovered []string
		for _, path := range conditionPaths {
			if len(truthyIn[path]) == 0 {
				uncovered = append(uncovered, fmt.Sprintf("%s is never true", path))
			}
			if _, unreachable := conditionsWithUnreachableFalseBranch[path]; unreachable {
				if len(falsyIn[path]) > 0 {
					uncovered = append(uncovered, fmt.Sprintf(
						"%s is listed in conditionsWithUnreachableFalseBranch, but the matrix entry %s renders it as "+
							"false; remove the entry from that list",
						path,
						falsyIn[path][0],
					))
				}
				continue
			}
			if len(falsyIn[path]) == 0 {
				uncovered = append(uncovered, fmt.Sprintf("%s is never false", path))
			}
		}
		Expect(uncovered).To(
			BeEmpty(),
			"the collector configuration matrix does not cover both branches of all template conditions; add or "+
				"adjust an entry in matrixKnobs so that the conditions listed above are rendered both ways",
		)
	})
})

// collectTemplateConditionPaths returns the field paths of all conditions in the collector configuration templates,
// relative to the template values, sorted and deduplicated.
func collectTemplateConditionPaths() []string {
	paths := map[string]struct{}{}
	for _, tmpl := range []*template.Template{
		daemonSetCollectorConfigurationTemplate,
		deploymentCollectorConfigurationTemplate,
		signalControlCollectorConfigurationTemplate,
	} {
		for _, associated := range tmpl.Templates() {
			if associated.Tree == nil {
				continue
			}
			collectConditionPathsFromList(associated.Tree.Root, paths)
		}
	}

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func collectConditionPathsFromList(list *parse.ListNode, paths map[string]struct{}) {
	if list == nil {
		return
	}
	for _, node := range list.Nodes {
		collectConditionPathsFromNode(node, paths)
	}
}

func collectConditionPathsFromNode(node parse.Node, paths map[string]struct{}) {
	switch typed := node.(type) {
	case *parse.IfNode:
		collectConditionPathsFromPipe(typed.Pipe, paths)
		collectConditionPathsFromList(typed.List, paths)
		collectConditionPathsFromList(typed.ElseList, paths)
	case *parse.RangeNode:
		collectConditionPathsFromPipe(typed.Pipe, paths)
		// The body of a range rebinds the dot, so field paths inside it do not refer to the template values.
	case *parse.WithNode:
		collectConditionPathsFromPipe(typed.Pipe, paths)
		// The body of a with rebinds the dot as well.
	case *parse.ListNode:
		collectConditionPathsFromList(typed, paths)
	}
}

func collectConditionPathsFromPipe(pipe *parse.PipeNode, paths map[string]struct{}) {
	if pipe == nil {
		return
	}
	for _, command := range pipe.Cmds {
		for _, argument := range command.Args {
			switch typed := argument.(type) {
			case *parse.FieldNode:
				paths[strings.Join(typed.Ident, ".")] = struct{}{}
			case *parse.PipeNode:
				collectConditionPathsFromPipe(typed, paths)
			}
		}
	}
}

// evaluateTemplateCondition resolves a field path against the template values and reports whether text/template would
// consider the result true, using the same notion of truth as {{ if }}.
func evaluateTemplateCondition(values *collectorConfigurationTemplateValues, path string) (bool, error) {
	current := reflect.ValueOf(values)
	for _, segment := range strings.Split(path, ".") {
		resolved, err := resolveTemplatePathSegment(current, segment)
		if err != nil {
			return false, fmt.Errorf("cannot resolve %q of the path %q: %w", segment, path, err)
		}
		current = resolved
	}

	truth, ok := template.IsTrue(current.Interface())
	if !ok {
		return false, fmt.Errorf("the value at the path %q has a type that cannot be used in a condition", path)
	}
	return truth, nil
}

func resolveTemplatePathSegment(value reflect.Value, segment string) (reflect.Value, error) {
	if result, ok := callTemplateMethod(value, segment); ok {
		return result, nil
	}
	if value.CanAddr() {
		if result, ok := callTemplateMethod(value.Addr(), segment); ok {
			return result, nil
		}
	}

	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, fmt.Errorf("the value is a nil pointer")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("the value is a %s, not a struct", value.Kind())
	}
	field := value.FieldByName(segment)
	if !field.IsValid() {
		return reflect.Value{}, fmt.Errorf("there is no such field or method")
	}
	return field, nil
}

func callTemplateMethod(value reflect.Value, name string) (reflect.Value, bool) {
	method := value.MethodByName(name)
	if !method.IsValid() {
		return reflect.Value{}, false
	}
	methodType := method.Type()
	if methodType.NumIn() != 0 || methodType.NumOut() != 1 {
		return reflect.Value{}, false
	}
	return method.Call(nil)[0], true
}
