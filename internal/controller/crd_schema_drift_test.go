// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

const crdBasesDirectory = "../../config/crd/bases"

var (
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	rawMessageType    = reflect.TypeFor[json.RawMessage]()
)

/*
schemaFacts describes one node of a JSON document schema: the JSON type, the enum values and whether the node is
required by its enclosing object. Facts are keyed by the JSON field path of the node, with "[]" appended for the items
of an array and "{}" for the values of a map, e.g. "contexts[]" for the items of the field "contexts".
*/
type schemaFacts struct {
	// jsonType is the JSON type of the node (string, integer, number, boolean, array, object, int-or-string), or
	// empty if the node holds arbitrary JSON.
	jsonType string
	hasEnum  bool
	enum     []string
	// enumInSomeVariants is set when only some variants of a client union restrict the node to an enum.
	enumInSomeVariants bool
	required           bool
	// isField is set for nodes that are a field of an object, as opposed to the items of an array or the values of a
	// map.
	isField bool
}

/*
clientUnionOccurrence records a union of the Dash0 API client (a type that keeps the raw JSON and offers one As...
method per variant) at the path where it occurs.
*/
type clientUnionOccurrence struct {
	key       string
	fieldPath string
	unionType reflect.Type
	variants  []reflect.Type
}

/*
crdSchemaDriftCheck compares a part of the OpenAPI schema of a generated custom resource definition with a type of the
Dash0 API client.
*/
type crdSchemaDriftCheck struct {
	// crdFile is the file name of the custom resource definition in config/crd/bases.
	crdFile string
	// version is the custom resource definition version to compare.
	version string
	// schemaPath is the path of the schema node to compare, relative to the root of the custom resource, e.g. "spec"
	// or "spec.slackConfig".
	schemaPath string
	// clientType is the Dash0 API client type to compare with the schema node.
	clientType reflect.Type
	// allowedDifferences maps the accepted differences (as rendered by crdSchemaDrift) to the reason why they are
	// accepted.
	allowedDifferences map[string]string
}

/*
crdSchemaDrift compares the schema of a custom resource definition with a type of the Dash0 API client and returns
every difference: fields that only exist on one side, differing JSON types, differing enum values and fields that are
only required on one side. Variants of a union in the API client are merged, since custom resources model a union as
one object that holds the fields of all variants, distinguished by a kind field and CEL validation rules.
*/
func crdSchemaDrift(
	schema *apiextensionsv1.JSONSchemaProps,
	clientType reflect.Type,
	enumValuesOf func(reflect.Type) ([]string, bool),
) []string {
	crdFacts := map[string]*schemaFacts{}
	collectCrdSchemaFacts(schema, "", "", crdFacts)
	clientFacts, _ := collectClientFacts(clientType, enumValuesOf)
	return compareSchemaFacts(crdFacts, clientFacts)
}

func collectCrdSchemaFacts(schema *apiextensionsv1.JSONSchemaProps, fieldPath string, key string, facts map[string]*schemaFacts) {
	if key != "" {
		f := facts[key]
		if f == nil {
			f = &schemaFacts{}
			facts[key] = f
		}
		f.jsonType = schema.Type
		if schema.XIntOrString {
			f.jsonType = "int-or-string"
		}
		for _, enumValue := range schema.Enum {
			var value any
			if err := json.Unmarshal(enumValue.Raw, &value); err != nil {
				Fail(fmt.Sprintf("cannot parse enum value %s at %s: %v", string(enumValue.Raw), key, err))
			}
			f.hasEnum = true
			f.enum = append(f.enum, fmt.Sprint(value))
		}
		slices.Sort(f.enum)
	}
	if schema.Items != nil && schema.Items.Schema != nil {
		collectCrdSchemaFacts(schema.Items.Schema, fieldPath, key+"[]", facts)
	}
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
		collectCrdSchemaFacts(schema.AdditionalProperties.Schema, fieldPath, key+"{}", facts)
	}
	for name, property := range schema.Properties {
		childPath := joinFieldPath(fieldPath, name)
		facts[childPath] = &schemaFacts{
			required: slices.Contains(schema.Required, name),
			isField:  true,
		}
		collectCrdSchemaFacts(&property, childPath, childPath, facts)
	}
}

func joinFieldPath(fieldPath string, name string) string {
	if fieldPath == "" {
		return name
	}
	return fieldPath + "." + name
}

/*
collectClientFacts collects the schema facts of a Dash0 API client type, and the unions it encounters on the way.
*/
func collectClientFacts(
	clientType reflect.Type,
	enumValuesOf func(reflect.Type) ([]string, bool),
) (map[string]*schemaFacts, []clientUnionOccurrence) {
	collector := &clientFactsCollector{enumValuesOf: enumValuesOf, visiting: map[reflect.Type]bool{}}
	facts := map[string]*schemaFacts{}
	collector.collect(clientType, "", "", facts)
	return facts, collector.unions
}

type clientFactsCollector struct {
	enumValuesOf func(reflect.Type) ([]string, bool)
	visiting     map[reflect.Type]bool
	unions       []clientUnionOccurrence
}

func (c *clientFactsCollector) collect(t reflect.Type, fieldPath string, key string, facts map[string]*schemaFacts) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if variants, isUnion := unionVariants(t); isUnion {
		c.unions = append(c.unions, clientUnionOccurrence{key: key, fieldPath: fieldPath, unionType: t, variants: variants})
		if len(variants) == 0 {
			return
		}
		if c.visiting[t] {
			return
		}
		c.visiting[t] = true
		defer delete(c.visiting, t)
		perVariant := make([]map[string]*schemaFacts, 0, len(variants))
		for _, variant := range variants {
			variantFacts := map[string]*schemaFacts{}
			c.collect(variant, fieldPath, key, variantFacts)
			perVariant = append(perVariant, variantFacts)
		}
		unionNode := facts[key]
		for mergedKey, merged := range mergeUnionVariantFacts(key, fieldPath, perVariant) {
			facts[mergedKey] = merged
		}
		if unionNode != nil && facts[key] != nil {
			// Whether the union is a required field is decided by the enclosing object, not by the variants.
			facts[key].required = unionNode.required
			facts[key].isField = unionNode.isField
		}
		return
	}

	jsonType := clientJsonType(t)
	if key != "" {
		f := facts[key]
		if f == nil {
			f = &schemaFacts{}
			facts[key] = f
		}
		f.jsonType = jsonType
		if t.Kind() == reflect.String {
			if values, isEnum := c.enumValuesOf(t); isEnum {
				f.hasEnum = true
				f.enum = slices.Sorted(slices.Values(values))
			}
		}
	}

	switch jsonType {
	case "array":
		c.collect(t.Elem(), fieldPath, key+"[]", facts)
	case "object":
		if t.Kind() == reflect.Map {
			c.collect(t.Elem(), fieldPath, key+"{}", facts)
			return
		}
		if c.visiting[t] {
			return
		}
		c.visiting[t] = true
		defer delete(c.visiting, t)
		c.collectStructFields(t, fieldPath, facts)
	}
}

func (c *clientFactsCollector) collectStructFields(t reflect.Type, fieldPath string, facts map[string]*schemaFacts) {
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
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			c.collectStructFields(embedded, fieldPath, facts)
			continue
		}
		_, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		childPath := joinFieldPath(fieldPath, name)
		facts[childPath] = &schemaFacts{
			required: !slices.Contains(strings.Split(options, ","), "omitempty") &&
				field.Type.Kind() != reflect.Pointer,
			isField: true,
		}
		c.collect(field.Type, childPath, childPath, facts)
	}
}

/*
unionVariants reports whether t is a union of the Dash0 API client, i.e. a struct that keeps the raw JSON in an
unexported field named union, and returns its variants, which are the return types of its As... methods.
*/
func unionVariants(t reflect.Type) ([]reflect.Type, bool) {
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	unionField, hasUnionField := t.FieldByName("union")
	if !hasUnionField || unionField.Type != rawMessageType {
		return nil, false
	}
	var variants []reflect.Type
	for i := range t.NumMethod() {
		method := t.Method(i)
		if !strings.HasPrefix(method.Name, "As") || method.Type.NumIn() != 1 || method.Type.NumOut() != 2 {
			continue
		}
		variants = append(variants, method.Type.Out(0))
	}
	return variants, true
}

func clientJsonType(t reflect.Type) string {
	if t == rawMessageType {
		return ""
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Array:
		if implementsMarshaler(t) {
			// for example uuid.UUID
			return "string"
		}
		return "array"
	case reflect.Slice:
		return "array"
	case reflect.Map:
		return "object"
	case reflect.Struct:
		if implementsMarshaler(t) && !hasJsonTaggedField(t) {
			// for example time.Time or openapi_types.Date
			return "string"
		}
		return "object"
	default:
		return ""
	}
}

func implementsMarshaler(t reflect.Type) bool {
	for _, marshaler := range []reflect.Type{jsonMarshalerType, textMarshalerType} {
		if t.Implements(marshaler) || reflect.PointerTo(t).Implements(marshaler) {
			return true
		}
	}
	return false
}

func hasJsonTaggedField(t reflect.Type) bool {
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			return true
		}
	}
	return false
}

/*
mergeUnionVariantFacts merges the schema facts of all variants of a union into the facts of one object that holds the
fields of all variants: a node has the JSON type of the variants if they agree on it, its enum values are the values of
all variants, and it is only required if every variant that has the enclosing object requires it.
*/
func mergeUnionVariantFacts(
	unionKey string,
	unionFieldPath string,
	perVariant []map[string]*schemaFacts,
) map[string]*schemaFacts {
	merged := map[string]*schemaFacts{}
	for _, variantFacts := range perVariant {
		for key := range variantFacts {
			if _, done := merged[key]; done {
				continue
			}
			parent, hasParent := enclosingKey(key)
			isUnionNode := key == unionKey || !hasParent || parent == unionFieldPath || parent == unionKey
			var occurrences []*schemaFacts
			variantsWithParent := 0
			for _, otherVariantFacts := range perVariant {
				if f, ok := otherVariantFacts[key]; ok {
					occurrences = append(occurrences, f)
				}
				if _, ok := otherVariantFacts[parent]; isUnionNode || ok {
					variantsWithParent++
				}
			}
			merged[key] = mergeOccurrences(occurrences, len(occurrences) == variantsWithParent)
		}
	}
	return merged
}

// enclosingKey returns the key of the node that contains the node with the given key.
func enclosingKey(key string) (string, bool) {
	for _, suffix := range []string{"[]", "{}"} {
		if trimmed, isElement := strings.CutSuffix(key, suffix); isElement {
			return trimmed, true
		}
	}
	return parentPath(key)
}

func mergeOccurrences(occurrences []*schemaFacts, inAllVariants bool) *schemaFacts {
	result := &schemaFacts{
		required: inAllVariants,
		isField:  occurrences[0].isField,
	}
	var jsonTypes []string
	enumVariants := 0
	for _, occurrence := range occurrences {
		if occurrence.jsonType != "" && !slices.Contains(jsonTypes, occurrence.jsonType) {
			jsonTypes = append(jsonTypes, occurrence.jsonType)
		}
		if occurrence.hasEnum {
			enumVariants++
			for _, value := range occurrence.enum {
				if !slices.Contains(result.enum, value) {
					result.enum = append(result.enum, value)
				}
			}
		}
		result.required = result.required && occurrence.required
		result.enumInSomeVariants = result.enumInSomeVariants || occurrence.enumInSomeVariants
	}
	slices.Sort(jsonTypes)
	result.jsonType = strings.Join(jsonTypes, "|")
	slices.Sort(result.enum)
	switch {
	case enumVariants == len(occurrences):
		result.hasEnum = true
	case enumVariants > 0:
		result.enumInSomeVariants = true
		result.enum = nil
	}
	return result
}

func compareSchemaFacts(crdFacts map[string]*schemaFacts, clientFacts map[string]*schemaFacts) []string {
	typeMismatches := map[string]bool{}
	for key, crd := range crdFacts {
		if client, inBoth := clientFacts[key]; inBoth && !jsonTypesMatch(crd.jsonType, client.jsonType) {
			typeMismatches[key] = true
		}
	}
	// The fields below a node whose type differs are a consequence of the type difference, which is reported once.
	belowTypeMismatch := func(path string) bool {
		parent, hasParent := parentPath(path)
		for hasParent {
			if typeMismatches[parent] || typeMismatches[parent+"[]"] || typeMismatches[parent+"{}"] {
				return true
			}
			parent, hasParent = parentPath(parent)
		}
		return false
	}

	crdFieldPaths := fieldPaths(crdFacts)
	clientFieldPaths := fieldPaths(clientFacts)
	var differences []string
	for _, path := range topmostMissingPaths(crdFieldPaths, clientFieldPaths) {
		if !belowTypeMismatch(path) {
			differences = append(differences, "only in CRD: "+path)
		}
	}
	for _, path := range topmostMissingPaths(clientFieldPaths, crdFieldPaths) {
		if !belowTypeMismatch(path) {
			differences = append(differences, "only in client: "+path)
		}
	}

	for key, crd := range crdFacts {
		client, inBoth := clientFacts[key]
		if !inBoth {
			continue
		}
		if crd.isField && client.isField && crd.required != client.required {
			if crd.required {
				differences = append(differences, "required only in CRD: "+key)
			} else {
				differences = append(differences, "required only in client: "+key)
			}
		}
		if !jsonTypesMatch(crd.jsonType, client.jsonType) {
			differences = append(differences,
				fmt.Sprintf("type: %s: %s in CRD, %s in client", key, crd.jsonType, client.jsonType))
			continue
		}
		differences = append(differences, compareEnums(key, crd, client)...)
	}
	slices.Sort(differences)
	return differences
}

func fieldPaths(facts map[string]*schemaFacts) map[string]bool {
	paths := map[string]bool{}
	for key, f := range facts {
		if f.isField {
			paths[key] = true
		}
	}
	return paths
}

func jsonTypesMatch(crdType string, clientType string) bool {
	if crdType == "" || clientType == "" || crdType == clientType {
		return true
	}
	return crdType == "int-or-string" && (clientType == "integer" || clientType == "string")
}

func compareEnums(key string, crd *schemaFacts, client *schemaFacts) []string {
	switch {
	case client.enumInSomeVariants:
		return []string{"enum only in some union variants: " + key}
	case crd.hasEnum && !client.hasEnum:
		return []string{"enum only in CRD: " + key}
	case !crd.hasEnum && client.hasEnum:
		return []string{"enum only in client: " + key}
	case !crd.hasEnum:
		return nil
	}
	var differences []string
	for _, value := range crd.enum {
		if !slices.Contains(client.enum, value) {
			differences = append(differences, fmt.Sprintf("enum value only in CRD: %s = %s", key, value))
		}
	}
	for _, value := range client.enum {
		if !slices.Contains(crd.enum, value) {
			differences = append(differences, fmt.Sprintf("enum value only in client: %s = %s", key, value))
		}
	}
	return differences
}

/*
loadCrdSchema returns the OpenAPI schema node at schemaPath (relative to the root of the custom resource, with fields
separated by ".") of the given version of the custom resource definition in config/crd/bases. Array items are
traversed implicitly.
*/
func loadCrdSchema(crdFile string, version string, schemaPath string) *apiextensionsv1.JSONSchemaProps {
	GinkgoHelper()
	content, err := os.ReadFile(filepath.Join(crdBasesDirectory, crdFile))
	Expect(err).ToNot(HaveOccurred())
	crd := apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal(content, &crd)).To(Succeed())
	for _, crdVersion := range crd.Spec.Versions {
		if crdVersion.Name != version {
			continue
		}
		schema := crdVersion.Schema.OpenAPIV3Schema
		for _, name := range strings.Split(schemaPath, ".") {
			for schema.Type == "array" && schema.Items != nil && schema.Items.Schema != nil {
				schema = schema.Items.Schema
			}
			property, ok := schema.Properties[name]
			Expect(ok).To(BeTrue(), "%s %s has no schema at %s", crdFile, version, schemaPath)
			schema = &property
		}
		return schema
	}
	Fail(fmt.Sprintf("%s has no version %s", crdFile, version))
	return nil
}

var (
	apiClientEnumValues     map[string][]string
	apiClientEnumValuesOnce sync.Once
	apiClientPackagePath    = reflect.TypeFor[dash0apiclient.ViewSpec]().PkgPath()
)

/*
apiClientEnumValuesOf returns the values of a string enum type of the Dash0 API client. The values are only available
as constants in the source code of the API client, which is why they are read by parsing the module's source files.
*/
func apiClientEnumValuesOf(t reflect.Type) ([]string, bool) {
	apiClientEnumValuesOnce.Do(func() {
		apiClientEnumValues = parseApiClientEnumValues()
	})
	if t.PkgPath() != apiClientPackagePath {
		return nil, false
	}
	values, ok := apiClientEnumValues[t.Name()]
	return values, ok
}

func parseApiClientEnumValues() map[string][]string {
	GinkgoHelper()
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", apiClientPackagePath).Output()
	Expect(err).ToNot(HaveOccurred(), "cannot locate the source code of %s", apiClientPackagePath)
	moduleDirectory := strings.TrimSpace(string(output))
	files, err := filepath.Glob(filepath.Join(moduleDirectory, "*.go"))
	Expect(err).ToNot(HaveOccurred())
	Expect(files).ToNot(BeEmpty(), "no Go files in %s", moduleDirectory)

	enumValues := map[string][]string{}
	fileSet := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		Expect(err).ToNot(HaveOccurred())
		for _, declaration := range parsed.Decls {
			genericDeclaration, ok := declaration.(*ast.GenDecl)
			if !ok || genericDeclaration.Tok != token.CONST {
				continue
			}
			for _, spec := range genericDeclaration.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				typeName, ok := valueSpec.Type.(*ast.Ident)
				if !ok || len(valueSpec.Values) != 1 {
					continue
				}
				literal, ok := valueSpec.Values[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				Expect(err).ToNot(HaveOccurred())
				enumValues[typeName.Name] = append(enumValues[typeName.Name], value)
			}
		}
	}
	return enumValues
}

/*
expectNoUnexpectedCrdSchemaDrift fails if the custom resource definition schema and the Dash0 API client type differ
in a way that is not listed in check.allowedDifferences, or if an allowed difference does not exist anymore.
*/
func expectNoUnexpectedCrdSchemaDrift(check crdSchemaDriftCheck) {
	GinkgoHelper()
	schema := loadCrdSchema(check.crdFile, check.version, check.schemaPath)
	differences := crdSchemaDrift(schema, check.clientType, apiClientEnumValuesOf)
	Expect(differences).To(ConsistOf(mapKeys(check.allowedDifferences)),
		"differences between the schema of %s %s at %s and %s; either align the custom resource definition with the "+
			"Dash0 API, or add the difference with a reason to the allowed differences",
		check.crdFile, check.version, check.schemaPath, check.clientType)
}

/*
uncoveredUnionVariants returns the variants of the API client unions that a set of drift checks does not compare with
a custom resource definition. A union is compared variant by variant if the custom resource definition has a field at
the path of the union, and a variant is compared on its own if it is the client type of one of the checks. Unions below
a path that does not exist in the custom resource definition are not considered, they are reported as fields that only
exist in the client.
*/
func uncoveredUnionVariants(checks []crdSchemaDriftCheck) []reflect.Type {
	GinkgoHelper()
	comparedOnTheirOwn := map[reflect.Type]bool{}
	for _, check := range checks {
		comparedOnTheirOwn[check.clientType] = true
	}
	var uncovered []reflect.Type
	for _, check := range checks {
		crdFacts := map[string]*schemaFacts{}
		collectCrdSchemaFacts(loadCrdSchema(check.crdFile, check.version, check.schemaPath), "", "", crdFacts)
		_, unions := collectClientFacts(check.clientType, apiClientEnumValuesOf)
		for _, union := range unions {
			if union.key == "" || crdFacts[union.key] != nil {
				continue
			}
			if parent, hasParent := parentPath(union.fieldPath); hasParent && crdFacts[parent] == nil {
				continue
			}
			for _, variant := range union.variants {
				if !comparedOnTheirOwn[variant] && !slices.Contains(uncovered, variant) {
					uncovered = append(uncovered, variant)
				}
			}
		}
	}
	return uncovered
}

type schemaDriftTestKind string

type schemaDriftTestFirstVariant struct {
	Kind schemaDriftTestKind `json:"kind"`
	Spec struct {
		Rate float32 `json:"rate"`
		Ottl *string `json:"ottl,omitempty"`
	} `json:"spec"`
}

type schemaDriftTestSecondVariant struct {
	Kind schemaDriftTestKind `json:"kind"`
	Spec struct {
		Nested struct {
			Name string `json:"name"`
		} `json:"nested"`
	} `json:"spec"`
}

type schemaDriftTestUnion struct {
	union json.RawMessage //nolint:unused
}

func (schemaDriftTestUnion) AsSchemaDriftTestFirstVariant() (schemaDriftTestFirstVariant, error) {
	return schemaDriftTestFirstVariant{}, nil
}

func (schemaDriftTestUnion) AsSchemaDriftTestSecondVariant() (schemaDriftTestSecondVariant, error) {
	return schemaDriftTestSecondVariant{}, nil
}

type schemaDriftTestClient struct {
	Type       schemaDriftTestKind    `json:"type"`
	Contexts   []schemaDriftTestKind  `json:"contexts"`
	Labels     *map[string]string     `json:"labels,omitempty"`
	Conditions []schemaDriftTestUnion `json:"conditions"`
	OnlyClient *string                `json:"onlyClient,omitempty"`
}

func schemaDriftTestEnumValues(t reflect.Type) ([]string, bool) {
	if t == reflect.TypeFor[schemaDriftTestKind]() {
		return []string{"first", "second", "third"}, true
	}
	return nil, false
}

func schemaDriftTestSchema(modify func(schema *apiextensionsv1.JSONSchemaProps)) *apiextensionsv1.JSONSchemaProps {
	enum := func(values ...string) []apiextensionsv1.JSON {
		result := make([]apiextensionsv1.JSON, 0, len(values))
		for _, value := range values {
			result = append(result, apiextensionsv1.JSON{Raw: []byte(strconv.Quote(value))})
		}
		return result
	}
	schema := &apiextensionsv1.JSONSchemaProps{
		Type:     "object",
		Required: []string{"type", "contexts", "conditions"},
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"type": {Type: "string", Enum: enum("first", "second", "third")},
			"contexts": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
				Schema: &apiextensionsv1.JSONSchemaProps{Type: "string", Enum: enum("first", "second", "third")},
			}},
			"labels": {Type: "object", AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{
				Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
			}},
			"conditions": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
				Schema: &apiextensionsv1.JSONSchemaProps{
					Type:     "object",
					Required: []string{"kind", "spec"},
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"kind": {Type: "string", Enum: enum("first", "second", "third")},
						"spec": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"rate": {Type: "number"},
								"ottl": {Type: "string"},
								"nested": {
									Type:       "object",
									Required:   []string{"name"},
									Properties: map[string]apiextensionsv1.JSONSchemaProps{"name": {Type: "string"}},
								},
							},
						},
					},
				},
			}},
			"onlyClient": {Type: "string"},
		},
	}
	if modify != nil {
		modify(schema)
	}
	return schema
}

var _ = Describe("Comparing custom resource definition schemas with Dash0 API client types", func() {
	drift := func(modify func(schema *apiextensionsv1.JSONSchemaProps)) []string {
		return crdSchemaDrift(
			schemaDriftTestSchema(modify),
			reflect.TypeFor[schemaDriftTestClient](),
			schemaDriftTestEnumValues,
		)
	}
	conditionSpec := func(schema *apiextensionsv1.JSONSchemaProps) *apiextensionsv1.JSONSchemaProps {
		condition := schema.Properties["conditions"].Items.Schema
		spec := condition.Properties["spec"]
		return &spec
	}
	setConditionSpec := func(schema *apiextensionsv1.JSONSchemaProps, spec *apiextensionsv1.JSONSchemaProps) {
		schema.Properties["conditions"].Items.Schema.Properties["spec"] = *spec
	}

	It("finds no drift between a schema and a client type that match, merging the variants of unions", func() {
		Expect(drift(nil)).To(BeEmpty())
	})

	It("reports fields that only exist on one side", func() {
		Expect(drift(func(schema *apiextensionsv1.JSONSchemaProps) {
			delete(schema.Properties, "onlyClient")
			schema.Properties["onlyCrd"] = apiextensionsv1.JSONSchemaProps{
				Type:       "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{"nested": {Type: "string"}},
			}
		})).To(Equal([]string{"only in CRD: onlyCrd", "only in client: onlyClient"}))
	})

	It("reports differing types, without reporting the fields below the differing node", func() {
		Expect(drift(func(schema *apiextensionsv1.JSONSchemaProps) {
			spec := conditionSpec(schema)
			spec.Properties["rate"] = apiextensionsv1.JSONSchemaProps{Type: "string"}
			spec.Properties["nested"] = apiextensionsv1.JSONSchemaProps{Type: "string"}
			setConditionSpec(schema, spec)
		})).To(Equal([]string{
			"type: conditions.spec.nested: string in CRD, object in client",
			"type: conditions.spec.rate: string in CRD, number in client",
		}))
	})

	It("reports enum values that only exist on one side, including the enum values of array items", func() {
		Expect(drift(func(schema *apiextensionsv1.JSONSchemaProps) {
			typeSchema := schema.Properties["type"]
			typeSchema.Enum = append(typeSchema.Enum[:1], apiextensionsv1.JSON{Raw: []byte(`"fourth"`)})
			schema.Properties["type"] = typeSchema
			contexts := schema.Properties["contexts"]
			contexts.Items.Schema.Enum = nil
			schema.Properties["contexts"] = contexts
		})).To(Equal([]string{
			"enum only in client: contexts[]",
			"enum value only in CRD: type = fourth",
			"enum value only in client: type = second",
			"enum value only in client: type = third",
		}))
	})

	It("reports fields that are only required on one side", func() {
		Expect(drift(func(schema *apiextensionsv1.JSONSchemaProps) {
			schema.Required = []string{"type", "contexts", "labels"}
		})).To(Equal([]string{
			"required only in CRD: labels",
			"required only in client: conditions",
		}))
	})

	It("treats a field of a union as required only if all variants that have the enclosing object require it", func() {
		Expect(drift(func(schema *apiextensionsv1.JSONSchemaProps) {
			spec := conditionSpec(schema)
			spec.Required = []string{"rate"}
			nested := spec.Properties["nested"]
			nested.Required = nil
			spec.Properties["nested"] = nested
			setConditionSpec(schema, spec)
		})).To(Equal([]string{
			"required only in CRD: conditions.spec.rate",
			"required only in client: conditions.spec.nested.name",
		}))
	})

	It("reports the unions of the client type with their variants", func() {
		_, unions := collectClientFacts(reflect.TypeFor[schemaDriftTestClient](), schemaDriftTestEnumValues)
		Expect(unions).To(HaveLen(1))
		Expect(unions[0].key).To(Equal("conditions[]"))
		Expect(unions[0].fieldPath).To(Equal("conditions"))
		Expect(unions[0].variants).To(ConsistOf(
			reflect.TypeFor[schemaDriftTestFirstVariant](),
			reflect.TypeFor[schemaDriftTestSecondVariant](),
		))
	})

	It("reads the enum values of the Dash0 API client from its source code", func() {
		values, isEnum := apiClientEnumValuesOf(reflect.TypeFor[dash0apiclient.SyntheticHttpErrorType]())
		Expect(isEnum).To(BeTrue())
		Expect(values).To(ContainElements("dns", "timeout"))
		_, isEnum = apiClientEnumValuesOf(reflect.TypeFor[dash0apiclient.Duration]())
		Expect(isEnum).To(BeFalse())
	})
})
