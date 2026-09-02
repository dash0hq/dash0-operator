// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	operatorv1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

// A heuristical test protecting against drift between agent0-connector secret redaction and CRD changes. It checks
// whether Dash0 custom resource types contain fields that might need secret redaction, and that are currently not
// redacted.

// redactionSourceFile is the path to the source file that implements the redaction in agent0-connector and holds the
// relevant lists. There is no Go module dependency between this package and agent0-connector, so the lists are read
// from the source code rather than imported.
var redactionSourceFile = filepath.Join(
	"..", "..", "images", "agent0-connector", "src", "kubectl", "redaction.go")

// crdBaseManifestDir is the path to the custom resource definitions generated from the Go types of this package (via
// "make manifests"). They declare the singular and the plural name of every resource type, which is what kubectl
// accepts and therefore what dash0ResourceTypesWithSecrets has to list; deriving the plural form from the kind here
// instead would only reproduce the guess a maintainer would make, which is exactly what needs checking.
var crdBaseManifestDir = filepath.Join("..", "..", "config", "crd", "bases")

// credentialNameFragments are the substrings that make a field name look like it holds a credential. The check is
// deliberately broad, to increase the likelihood of flagging unredacted secrets. For a field not to be flagged it needs
// to be either covered by agent0-connector's redaction, or listed in knownUnredactedFields.
// "header" is included because a header value is a credential wherever a request carries one, and a field can be named
// after the header rather than after what it holds (IncidentioConfig.Headers is the Incident.io authorization header
// value). "signature", "hmac", "bearer" and "cookie" are the other names a credential is commonly given that none of
// the remaining fragments match.
var credentialNameFragments = []string{
	"token", "pass", "key", "secret", "url", "credential", "auth",
	"header", "signature", "hmac", "bearer", "cookie",
}

// knownUnredactedFields are the fields that credentialNameFragments flags, although they actually hold no credential.
// The key is "<enclosing object>.<field>".
var knownUnredactedFields = map[string]string{
	// The endpoint a PagerDuty integration posts to. Unlike the webhook URLs of the other channel types it carries no
	// token; the credential of this channel is pagerdutyConfig.key, which is redacted.
	"pagerdutyConfig.url": "the public PagerDuty events endpoint, not an unguessable URL",
	// The name of the entry within a Kubernetes secret, not its value. The value never reaches the response: the
	// operator resolves it into the workload, and the custom resource only ever holds the reference.
	"secretKeyRef.key": "the name of an entry in a Kubernetes secret, not its value",
	"secretRef.key":    "the name of an entry in a Kubernetes secret, not its value",
	// Attribute keys of filters, columns and assertions - the left-hand side of a comparison, never a secret.
	"filter.key":         "an attribute key of a filter",
	"filters.key":        "an attribute key of a filter",
	"implicitFilter.key": "an attribute key of a filter",
	"columns.key":        "an attribute key of a table column",
	"sort.key":           "an attribute key of a sort order",
	"spec.key":           "the response header name of a synthetic check assertion",
}

// TestAgent0ConnectorRedactsEveryCredentialField guards the credential lists of the agent0-connector
// (images/agent0-connector/src/kubectl/redaction.go) against drift. The lists in redaction.go are a hand-maintained
// copy of implicit knowledge about the custom resource types of this package. This test is a best-effort attempt to
// bind the CRDs to the redaction lists.
func TestAgent0ConnectorRedactsEveryCredentialField(t *testing.T) {
	resourceTypesWithSecrets, coveredFieldsPerConfigObject, fieldsThatAreRedactedEverywhere :=
		parseRedactionLists(t)

	var uncovered []string
	credentialFieldsPerKind := map[string][]string{}

	for _, field := range allPotentialCredentialFields(t) {
		if _, allowed := knownUnredactedFields[field.enclosingObject+"."+field.name]; allowed {
			continue
		}

		_, handledAnywhere := fieldsThatAreRedactedEverywhere[field.name]
		handledInObject := slices.Contains(coveredFieldsPerConfigObject[field.enclosingObject], field.name)
		if !handledAnywhere && !handledInObject {
			uncovered = append(uncovered, field.path+" (field "+field.name+" in "+field.enclosingObject+")")
			continue
		}
		credentialFieldsPerKind[field.kind] = append(credentialFieldsPerKind[field.kind], field.path)
	}

	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		t.Errorf(
			"%d credential-like field(s) of the custom resources are not redacted by the agent0-connector:\n  %s\n\n"+
				"Either add them to %s - to credentialFieldsPerConfigObject when the field only holds a credential "+
				"within that configuration object, to urlFieldsPerConfigObject when it holds a URL that is not itself "+
				"a credential but can carry one, or to the field names handled in redactDocumentNodeRecursively when "+
				"it holds a credential wherever it occurs. Or, if the value is not a credential, add it to "+
				"knownUnredactedFields in this test with the reason why it does not need redaction.",
			len(uncovered),
			strings.Join(uncovered, "\n  "),
			redactionSourceFile,
		)
	}

	resourceNamesPerKind := resourceNamesFromCrdManifests(t)

	for kind, credentialFields := range credentialFieldsPerKind {
		// Every name kubectl accepts for the resource type has to be listed: normalizeResourceType in the connector's
		// parser only lower-cases the resource type and strips the API group suffix, it never singularizes. So a missing
		// plural form - the form most requests use - means the connector neither redacts the responses that render this
		// resource type, nor rejects the output formats whose result it cannot redact.
		acceptedNames := resourceNamesPerKind[kind]
		if len(acceptedNames) == 0 {
			// No CRD declares this kind, so the singular form derived from the kind is all that can be checked. kubectl
			// also accepts the kind itself, which normalizes to exactly that.
			acceptedNames = []string{strings.ToLower(kind)}
		}

		var missing []string
		for _, name := range acceptedNames {
			if _, covered := resourceTypesWithSecrets[name]; !covered {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			continue
		}

		sort.Strings(credentialFields)
		sort.Strings(missing)
		t.Errorf(
			"%s contains potential credentials, but %d of the resource type names kubectl accepts for it are not "+
				"listed in dash0ResourceTypesWithSecrets in %s: %s. Add every one of them (the singular and the plural "+
				"form): without them the connector neither redacts the responses that render this resource type, nor "+
				"rejects the output formats whose result it cannot redact. Or, if the values are not credentials, add "+
				"them to knownUnredactedFields in this test with the reason why they do not need redaction. The "+
				"field(s) that hold a potential credential are:\n  %s",
			kind,
			len(missing),
			redactionSourceFile,
			strings.Join(missing, ", "),
			strings.Join(credentialFields, "\n  "),
		)
	}
}

// resourceNamesFromCrdManifests returns, per kind, the resource type names kubectl accepts for it: the singular and the
// plural name declared by the generated custom resource definitions, plus the lower-cased kind, which is the form
// kubectl's kind argument normalizes to. Short names are included as well, so that adding one to a credential-bearing
// CRD without extending dash0ResourceTypesWithSecrets fails this test rather than silently opening a bypass.
func resourceNamesFromCrdManifests(t *testing.T) map[string][]string {
	t.Helper()

	manifests, err := filepath.Glob(filepath.Join(crdBaseManifestDir, "*.yaml"))
	if err != nil {
		t.Fatalf("cannot list the CRD manifests in %s: %v", crdBaseManifestDir, err)
	}
	if len(manifests) == 0 {
		t.Fatalf(
			"no CRD manifests found in %s; this test derives the accepted resource type names from them, run "+
				"\"make manifests generate\"",
			crdBaseManifestDir,
		)
	}

	namesPerKind := map[string][]string{}
	for _, manifest := range manifests {
		content, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("cannot read the CRD manifest %s: %v", manifest, err)
		}
		// sigs.k8s.io/yaml converts YAML to JSON before unmarshalling, so the JSON tags apply.
		var crd struct {
			Spec struct {
				Names struct {
					Kind       string   `json:"kind"`
					Singular   string   `json:"singular"`
					Plural     string   `json:"plural"`
					ShortNames []string `json:"shortNames"`
				} `json:"names"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal(content, &crd); err != nil {
			t.Fatalf("cannot parse the CRD manifest %s: %v", manifest, err)
		}
		names := crd.Spec.Names
		if names.Kind == "" {
			t.Fatalf("the CRD manifest %s declares no spec.names.kind", manifest)
		}
		accepted := []string{strings.ToLower(names.Kind)}
		for _, name := range append([]string{names.Singular, names.Plural}, names.ShortNames...) {
			if name != "" && !slices.Contains(accepted, name) {
				accepted = append(accepted, name)
			}
		}
		namesPerKind[names.Kind] = accepted
	}
	return namesPerKind
}

// potentialCredentialField is a field of a custom resource whose name indicates it might hold a credential.
type potentialCredentialField struct {
	kind            string
	name            string
	enclosingObject string
	path            string
}

// allPotentialCredentialFields walks the custom resource types registered in the schemes of this repository and returns
// every string-typed field whose name matches credentialNameFragments.
func allPotentialCredentialFields(t *testing.T) []potentialCredentialField {
	t.Helper()

	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		operatorv1alpha1.AddToScheme,
		operatorv1beta1.AddToScheme,
		dash0v1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("cannot build the scheme: %v", err)
		}
	}

	var fields []potentialCredentialField
	for groupVersionKind, resourceType := range scheme.AllKnownTypes() {
		if strings.HasSuffix(groupVersionKind.Kind, "List") {
			continue
		}
		collectPotentialCredentialFields(
			resourceType,
			groupVersionKind.Kind,
			groupVersionKind.Kind,
			"",
			map[reflect.Type]struct{}{},
			&fields,
		)
	}
	if len(fields) == 0 {
		t.Fatal("no credential-like fields found at all, the walk over the custom resource types is broken")
	}
	return fields
}

func collectPotentialCredentialFields(
	resourceType reflect.Type,
	kind string,
	path string,
	enclosingObject string,
	visited map[reflect.Type]struct{},
	fields *[]potentialCredentialField,
) {
	resourceType = elementTypeOf(resourceType)
	if resourceType.Kind() == reflect.Map {
		collectPotentialCredentialFields(resourceType.Elem(), kind, path+".*", enclosingObject, visited, fields)
		return
	}
	if resourceType.Kind() != reflect.Struct {
		return
	}
	// Guards against the recursive types some custom resources use (e.g. a filter that holds filters).
	if _, alreadyVisited := visited[resourceType]; alreadyVisited {
		return
	}
	visited[resourceType] = struct{}{}
	defer delete(visited, resourceType)

	for i := range resourceType.NumField() {
		field := resourceType.Field(i)
		name := jsonFieldName(field)
		if name == "" {
			continue
		}
		fieldType := elementTypeOf(field.Type)
		fieldPath := path + "." + name
		// A string holds a credential value directly, and a map of strings holds one per entry - the shape of the generic
		// webhook channel's headers. Both are value-shaped, so the name of the field decides whether it might be a
		// credential. A struct is a container instead: its own name says nothing, so its fields are inspected on their
		// own. That is a known limit of this heuristic, since a list of name/value pairs (the shape of the export headers
		// and of a synthetic check's headers) is only visible through its "name" and "value" fields, which match no
		// fragment.
		if fieldType.Kind() == reflect.String || isMapOfStrings(fieldType) {
			if hasCredentialLikeName(name) {
				*fields = append(*fields, potentialCredentialField{
					kind:            kind,
					name:            name,
					enclosingObject: enclosingObject,
					path:            fieldPath,
				})
			}
			continue
		}
		collectPotentialCredentialFields(fieldType, kind, fieldPath, name, visited, fields)
	}
}

// isMapOfStrings reports whether the given type is a map whose values are strings, i.e. a field that holds a credential
// value per entry rather than containing further fields.
func isMapOfStrings(t reflect.Type) bool {
	return t.Kind() == reflect.Map && elementTypeOf(t.Elem()).Kind() == reflect.String
}

// elementTypeOf unwraps pointers, slices and arrays, so that a field is inspected by the type it ultimately holds.
func elementTypeOf(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return t
}

// jsonFieldName returns the name a field is serialized under, or "" when it is not serialized on its own (an inlined
// or omitted field).
func jsonFieldName(field reflect.StructField) string {
	name := strings.Split(field.Tag.Get("json"), ",")[0]
	if name == "-" {
		return ""
	}
	return name
}

func hasCredentialLikeName(name string) bool {
	lowerCased := strings.ToLower(name)
	for _, fragment := range credentialNameFragments {
		if strings.Contains(lowerCased, fragment) {
			return true
		}
	}
	return false
}

// parseRedactionLists reads the credential lists of the agent0-connector from its source: the field names that are
// redacted wherever they occur (the case clauses of redactDocumentNodeRecursively), the fields that are only redacted
// within a particular configuration object (credentialFieldsPerConfigObject and urlFieldsPerConfigObject, merged into
// one map), and the resource types whose content can contain a credential (dash0ResourceTypesWithSecrets).
func parseRedactionLists(t *testing.T) (map[string]struct{}, map[string][]string, map[string]struct{}) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), redactionSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", redactionSourceFile, err)
	}

	resourceTypesWithSecrets := map[string]struct{}{}
	coveredFieldsPerConfigObject := map[string][]string{}
	fieldsThatAreRedactedEverywhere := map[string]struct{}{}
	var credentialFieldListsFound []string

	ast.Inspect(file, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.FuncDecl:
			if typedNode.Name.Name == "redactDocumentNodeRecursively" {
				for _, name := range caseClauseStrings(typedNode) {
					fieldsThatAreRedactedEverywhere[name] = struct{}{}
				}
			}
		case *ast.ValueSpec:
			for i, name := range typedNode.Names {
				if i >= len(typedNode.Values) {
					continue
				}
				literal, isComposite := typedNode.Values[i].(*ast.CompositeLit)
				if !isComposite {
					continue
				}
				switch name.Name {
				case "credentialFieldsPerConfigObject", "urlFieldsPerConfigObject":
					credentialFieldListsFound = append(credentialFieldListsFound, name.Name)
					for key, element := range mapLiteralEntries(literal) {
						coveredFieldsPerConfigObject[key] = append(
							coveredFieldsPerConfigObject[key], compositeLitStrings(element)...)
					}
				case "dash0ResourceTypesWithSecrets":
					for key := range mapLiteralEntries(literal) {
						resourceTypesWithSecrets[key] = struct{}{}
					}
				}
			}
		}
		return true
	})

	// A restructuring of redaction.go that this parser no longer understands must fail rather than report an empty list,
	// which would make every field below look uncovered or - worse - every list look complete.
	if len(fieldsThatAreRedactedEverywhere) == 0 {
		t.Fatalf("no field names found in redactDocumentNodeRecursively in %s", redactionSourceFile)
	}
	for _, listName := range []string{"credentialFieldsPerConfigObject", "urlFieldsPerConfigObject"} {
		if !slices.Contains(credentialFieldListsFound, listName) {
			t.Fatalf("%s not found in %s", listName, redactionSourceFile)
		}
	}
	if len(resourceTypesWithSecrets) == 0 {
		t.Fatalf("dash0ResourceTypesWithSecrets not found in %s", redactionSourceFile)
	}
	return resourceTypesWithSecrets, coveredFieldsPerConfigObject, fieldsThatAreRedactedEverywhere
}

// caseClauseStrings returns the string literals of every case clause in the given function.
func caseClauseStrings(function *ast.FuncDecl) []string {
	var names []string
	ast.Inspect(function, func(node ast.Node) bool {
		caseClause, isCaseClause := node.(*ast.CaseClause)
		if !isCaseClause {
			return true
		}
		for _, expression := range caseClause.List {
			if name, isString := stringLiteral(expression); isString {
				names = append(names, name)
			}
		}
		return true
	})
	return names
}

// mapLiteralEntries returns the entries of a map literal, keyed by the string keys of that literal.
func mapLiteralEntries(literal *ast.CompositeLit) map[string]ast.Expr {
	entries := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		keyValue, isKeyValue := element.(*ast.KeyValueExpr)
		if !isKeyValue {
			continue
		}
		if key, isString := stringLiteral(keyValue.Key); isString {
			entries[key] = keyValue.Value
		}
	}
	return entries
}

// compositeLitStrings returns the string literals of a composite literal, e.g. the fields of a {"url"} entry.
func compositeLitStrings(expression ast.Expr) []string {
	literal, isComposite := expression.(*ast.CompositeLit)
	if !isComposite {
		return nil
	}
	var values []string
	for _, element := range literal.Elts {
		if value, isString := stringLiteral(element); isString {
			values = append(values, value)
		}
	}
	return values
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
