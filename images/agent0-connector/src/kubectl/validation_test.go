// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"fmt"
	"testing"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

// The rejection reasons validateCommandAndParseArguments can reject a request with, one builder per check. A test case
// that expects a rejection has to declare the reason it expects, so that it cannot pass because an unrelated check
// rejected the request first.
//
// The messages are spelled out here instead of being shared with validation.go, so that a reworded message shows up as
// a failing test and has to be confirmed deliberately.

func commandNotAllowed(command string) string {
	return fmt.Sprintf("only the \"kubectl\" command is allowed, but got %q", command)
}

func flagNotAllowed(flag string) string {
	return fmt.Sprintf("the kubectl flag %q is not allowed", flag)
}

func subcommandNotAllowed(subcommand string) string {
	return fmt.Sprintf("the kubectl subcommand %q is not an allowed read-only command", subcommand)
}

// The subcommand is spelled out by the caller like in every other builder, even though "cluster-info" happens to be
// the only bare-only subcommand today.
//
//nolint:unparam
func subcommandAllowedBareOnly(subcommand string, requestedSubVerb string) string {
	return fmt.Sprintf(
		"the kubectl subcommand %q must be used without additional arguments, but got %q; its sub-commands render "+
			"content in a format whose secrets cannot be redacted",
		subcommand,
		requestedSubVerb,
	)
}

func subVerbNotAllowed(subcommand string, allowedSubVerbs string, requestedSubVerb string) string {
	return fmt.Sprintf(
		"the kubectl subcommand %q is only allowed with the sub-command %q, but got %q instead",
		subcommand,
		allowedSubVerbs,
		requestedSubVerb,
	)
}

func outputFormatNotAllowed(format string) string {
	return fmt.Sprintf("the kubectl output format %q is not allowed", format)
}

func contentExposingSubcommand(subcommand string, resource string) string {
	return fmt.Sprintf("the kubectl subcommand %q prints the contents of a %s, which is not allowed", subcommand, resource)
}

func contentsNotReadable(resource string) string {
	return fmt.Sprintf(
		"reading the contents of a %s is not allowed; listing %ss or checking for the presence of a particular %s is "+
			"supported, but serializing its data (e.g. via -o yaml/json/jsonpath/go-template/custom-columns) is not",
		resource,
		resource,
		resource,
	)
}

func outputFormatNotRedactable(format string) string {
	return fmt.Sprintf(
		"the output format %q cannot be redacted reliably for a Dash0 custom resource, which can contain an "+
			"authorization token or third-party credentials; reading such a resource is supported with "+
			"-o json/yaml/name/wide (or without an output format), but not with a format that can reshape its values "+
			"(-o go-template/template/jsonpath/jsonpath-as-json/custom-columns or --template)",
		format,
	)
}

const describeOfDash0ResourceNotSupported = "describing a Dash0 custom resource is not supported, because it can " +
	"contain an authorization token or third-party credentials which cannot be redacted from the output of " +
	"\"kubectl describe\"; read the resource with \"kubectl get ... -o yaml\" or \"-o json\" instead, which returns " +
	"the same content with its credentials redacted, and its events with " +
	"\"kubectl events --for <resource-type>/<name>\""

//nolint:lll
func TestValidateCommandRequest(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		arguments []string
		allowed   bool
		// rejectionReason is the error message the request must be rejected with. It is required for every case with
		// allowed: false, and must be empty otherwise.
		rejectionReason string
	}{
		{name: "no subcommand (bare kubectl) is allowed", command: "kubectl", arguments: nil, allowed: true},
		{name: "no subcommand (empty arguments) is allowed", command: "kubectl", arguments: []string{}, allowed: true},
		{name: "global help flag without subcommand is allowed", command: "kubectl", arguments: []string{"--help"}, allowed: true},

		{name: "read-only get is allowed", command: "kubectl", arguments: []string{"get", "pods"}, allowed: true},
		{name: "get with -n flag is allowed", command: "kubectl", arguments: []string{"get", "po", "-n", "x"}, allowed: true},
		{name: "describe is allowed", command: "kubectl", arguments: []string{"describe", "pod", "x"}, allowed: true},
		{name: "logs is allowed", command: "kubectl", arguments: []string{"logs", "x"}, allowed: true},
		{name: "version is allowed", command: "kubectl", arguments: []string{"version"}, allowed: true},
		{name: "explain is allowed", command: "kubectl", arguments: []string{"explain", "pods"}, allowed: true},
		{name: "api-resources is allowed", command: "kubectl", arguments: []string{"api-resources"}, allowed: true},
		{name: "api-versions is allowed", command: "kubectl", arguments: []string{"api-versions"}, allowed: true},
		{name: "cluster-info is allowed", command: "kubectl", arguments: []string{"cluster-info"}, allowed: true},
		// "kubectl cluster-info dump" writes the full JSON of the workloads of a namespace (or of the whole cluster)
		// plus the raw logs of every container to stdout. Redaction does not run for a subcommand other than "get", and
		// the response is not a document that could be parsed anyway, so the pod specs of the operator's own workloads
		// would hand out the Dash0 auth token they carry as a literal env var value.
		{name: "cluster-info dump is rejected", command: "kubectl", arguments: []string{"cluster-info", "dump"}, allowed: false,
			rejectionReason: subcommandAllowedBareOnly("cluster-info", "dump")},
		{name: "cluster-info dump for all namespaces is rejected", command: "kubectl", arguments: []string{"cluster-info", "dump", "-A"}, allowed: false,
			rejectionReason: subcommandAllowedBareOnly("cluster-info", "dump")},
		{name: "cluster-info dump with a leading flag is rejected", command: "kubectl", arguments: []string{"-n", "dash0-system", "cluster-info", "dump"}, allowed: false,
			rejectionReason: subcommandAllowedBareOnly("cluster-info", "dump")},
		{name: "cluster-info dump with an output format is rejected", command: "kubectl", arguments: []string{"cluster-info", "dump", "-o", "json"}, allowed: false,
			rejectionReason: subcommandAllowedBareOnly("cluster-info", "dump")},
		// Any positional argument is rejected, so a sub-verb added by a future kubectl release is rejected as well.
		{name: "an unknown cluster-info sub-verb is rejected", command: "kubectl", arguments: []string{"cluster-info", "somethingelse"}, allowed: false,
			rejectionReason: subcommandAllowedBareOnly("cluster-info", "somethingelse")},
		{name: "cluster-info with only flags stays allowed", command: "kubectl", arguments: []string{"cluster-info", "--help"}, allowed: true},
		{name: "top is allowed", command: "kubectl", arguments: []string{"top", "pods"}, allowed: true},
		{name: "auth can-i is allowed", command: "kubectl", arguments: []string{"auth", "can-i", "get", "pods"},
			allowed: true},
		{name: "auth can-i --list is allowed", command: "kubectl", arguments: []string{"auth", "can-i", "--list"},
			allowed: true},
		{name: "auth reconcile is rejected",
			command: "kubectl", arguments: []string{"auth", "reconcile"}, allowed: false,
			rejectionReason: subVerbNotAllowed("auth", "can-i", "reconcile")},
		{name: "auth whoami is rejected", command: "kubectl", arguments: []string{"auth", "whoami"}, allowed: false,
			rejectionReason: subVerbNotAllowed("auth", "can-i", "whoami")},
		{name: "bare auth is rejected", command: "kubectl", arguments: []string{"auth"}, allowed: false,
			rejectionReason: subVerbNotAllowed("auth", "can-i", "")},
		{name: "events is allowed", command: "kubectl", arguments: []string{"events"}, allowed: true},

		{name: "non-kubectl command is rejected", command: "helm", arguments: []string{"list"}, allowed: false,
			rejectionReason: commandNotAllowed("helm")},
		{name: "empty command is rejected", command: "", arguments: []string{"get", "pods"}, allowed: false,
			rejectionReason: commandNotAllowed("")},
		{name: "mutating delete is rejected", command: "kubectl", arguments: []string{"delete", "pod", "x"}, allowed: false,
			rejectionReason: subcommandNotAllowed("delete")},
		{name: "mutating apply is rejected", command: "kubectl", arguments: []string{"apply"}, allowed: false,
			rejectionReason: subcommandNotAllowed("apply")},
		{name: "the file flag of a mutating subcommand is rejected as well", command: "kubectl", arguments: []string{"apply", "-f", "x"}, allowed: false,
			rejectionReason: flagNotAllowed("-f")},
		{name: "mutating edit is rejected", command: "kubectl", arguments: []string{"edit", "deploy", "x"}, allowed: false,
			rejectionReason: subcommandNotAllowed("edit")},
		{name: "leading value-taking flag before subcommand is allowed", command: "kubectl", arguments: []string{"-n", "x", "get", "po"}, allowed: true},
		{name: "leading flag in --flag=value form before subcommand is allowed", command: "kubectl", arguments: []string{"--namespace=x", "get", "po"}, allowed: true},
		{name: "leading boolean flag before subcommand is allowed", command: "kubectl", arguments: []string{"--no-headers=true", "get", "po"}, allowed: true},
		{name: "leading flags before a mutating subcommand are still rejected", command: "kubectl", arguments: []string{"-n", "x", "delete", "pod", "y"}, allowed: false,
			rejectionReason: subcommandNotAllowed("delete")},
		{name: "sensitive flag before subcommand is still rejected", command: "kubectl", arguments: []string{"--kubeconfig=/x", "get", "po"}, allowed: false,
			rejectionReason: flagNotAllowed("--kubeconfig=/x")},
		{name: "watch flag (-w) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-w"}, allowed: false,
			rejectionReason: flagNotAllowed("-w")},
		{name: "--watch is rejected", command: "kubectl", arguments: []string{"get", "po", "--watch"}, allowed: false,
			rejectionReason: flagNotAllowed("--watch")},
		{name: "--watch-only is rejected", command: "kubectl", arguments: []string{"get", "pods", "--watch-only"}, allowed: false,
			rejectionReason: flagNotAllowed("--watch-only")},
		{name: "--watch=true is rejected", command: "kubectl", arguments: []string{"get", "--watch=true"}, allowed: false,
			rejectionReason: flagNotAllowed("--watch=true")},
		{name: "--watch-only=true is rejected", command: "kubectl", arguments: []string{"get", "--watch-only=true"}, allowed: false,
			rejectionReason: flagNotAllowed("--watch-only=true")},

		{name: "follow flag (-f) is rejected", command: "kubectl", arguments: []string{"logs", "x", "-f"}, allowed: false,
			rejectionReason: flagNotAllowed("-f")},
		{name: "--follow is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow"}, allowed: false,
			rejectionReason: flagNotAllowed("--follow")},
		{name: "--follow=true is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow=true"}, allowed: false,
			rejectionReason: flagNotAllowed("--follow=true")},

		// Flags outside the allowlist are rejected, whatever they are for; --raw would otherwise turn "get" into an
		// arbitrary API request, bypassing the resource-based secret check below.
		{name: "--raw is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/secrets/my-secret"}, allowed: false,
			rejectionReason: flagNotAllowed("--raw")},
		{name: "--raw is rejected for any type", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/pods/my-pod"}, allowed: false,
			rejectionReason: flagNotAllowed("--raw")},
		{name: "--raw=value is rejected", command: "kubectl", arguments: []string{"get", "--raw=/api/v1/namespaces/default/secrets/my-secret"}, allowed: false,
			rejectionReason: flagNotAllowed("--raw=/api/v1/namespaces/default/secrets/my-secret")},
		{name: "--raw for a cluster-wide collection is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/secrets"}, allowed: false,
			rejectionReason: flagNotAllowed("--raw")},
		{name: "--raw for a configmap is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/configmaps/my-cm"}, allowed: false,
			rejectionReason: flagNotAllowed("--raw")},
		{name: "an unknown flag is rejected", command: "kubectl", arguments: []string{"get", "pods", "--show-managed-fields-typo"}, allowed: false,
			rejectionReason: flagNotAllowed("--show-managed-fields-typo")},
		// -v=8 and above make kubectl log the HTTP response bodies, exposing the contents of any resource on stderr.
		{name: "verbosity flag is rejected", command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-v=9"}, allowed: false,
			rejectionReason: flagNotAllowed("-v=9")},
		{name: "verbosity flag with a separate value is rejected", command: "kubectl", arguments: []string{"get", "cm", "-v", "8"}, allowed: false,
			rejectionReason: flagNotAllowed("-v")},
		{name: "long verbosity flag is rejected", command: "kubectl", arguments: []string{"get", "pods", "--v=9"}, allowed: false,
			rejectionReason: flagNotAllowed("--v=9")},
		{name: "a grouped shorthand with an unknown member is rejected", command: "kubectl", arguments: []string{"get", "pods", "-Az"}, allowed: false,
			rejectionReason: flagNotAllowed("-Az")},
		{name: "--filename is rejected", command: "kubectl", arguments: []string{"get", "--filename", "pod.yaml"}, allowed: false,
			rejectionReason: flagNotAllowed("--filename")},
		{name: "the kustomize shorthand (-k) is rejected", command: "kubectl", arguments: []string{"get", "-k", "dir"}, allowed: false,
			rejectionReason: flagNotAllowed("-k")},
		{name: "the end-of-flags separator is rejected", command: "kubectl", arguments: []string{"get", "pods", "--"}, allowed: false,
			rejectionReason: flagNotAllowed("--")},
		{name: "a bare dash is rejected", command: "kubectl", arguments: []string{"get", "pods", "-"}, allowed: false,
			rejectionReason: flagNotAllowed("-")},

		{name: "impersonation (--as) is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as", "system:admin"}, allowed: false,
			rejectionReason: flagNotAllowed("--as")},
		{name: "--as-group is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as-group=system:masters"}, allowed: false,
			rejectionReason: flagNotAllowed("--as-group=system:masters")},
		{name: "--server is rejected", command: "kubectl", arguments: []string{"get", "pods", "--server", "https://evil"}, allowed: false,
			rejectionReason: flagNotAllowed("--server")},
		{name: "-s (server short flag) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-s", "https://evil"}, allowed: false,
			rejectionReason: flagNotAllowed("-s")},
		{name: "--kubeconfig is rejected", command: "kubectl", arguments: []string{"get", "pods", "--kubeconfig=/x"}, allowed: false,
			rejectionReason: flagNotAllowed("--kubeconfig=/x")},
		{name: "--context is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context", "other"}, allowed: false,
			rejectionReason: flagNotAllowed("--context")},
		{name: "--context=value is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context=other"}, allowed: false,
			rejectionReason: flagNotAllowed("--context=other")},
		{name: "--token is rejected", command: "kubectl", arguments: []string{"get", "pods", "--token", "abc"}, allowed: false,
			rejectionReason: flagNotAllowed("--token")},
		{name: "--insecure-skip-tls-verify is rejected", command: "kubectl", arguments: []string{"get", "pods", "--insecure-skip-tls-verify"}, allowed: false,
			rejectionReason: flagNotAllowed("--insecure-skip-tls-verify")},

		// Output formats: the value of -o/--output is checked against an allowlist. The formats that take their
		// template from a file render that file, so they would read an arbitrary file from the connector's own
		// container; they are rejected for every resource type, sensitive or not.
		{name: "-o go-template-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("go-template-file")},
		{name: "-o go-template-file with an attached value is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-ogo-template-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("go-template-file")},
		{name: "-o go-template-file taking its path from --template is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file", "--template", "/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("go-template-file")},
		{name: "-o templatefile is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "templatefile", "--template=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("templatefile")},
		{name: "-o jsonpath-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("jsonpath-file")},
		{name: "--output=jsonpath-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "--output=jsonpath-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("jsonpath-file")},
		{name: "-o custom-columns-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "custom-columns-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("custom-columns-file")},
		{name: "a file output format in a grouped shorthand is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-Aocustom-columns-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("custom-columns-file")},
		{name: "a file output format reading the service account token is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file=/var/run/secrets/kubernetes.io/serviceaccount/token"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("go-template-file")},
		{name: "a file output format is rejected even when overridden by an allowed one",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath-file=/etc/passwd", "-o", "name"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("jsonpath-file")},
		{name: "a file output format is rejected even when it overrides an allowed one",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "name", "-o", "jsonpath-file=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("jsonpath-file")},
		{name: "an unknown output format is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "bogusformat"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("bogusformat")},

		{name: "-o json is allowed", command: "kubectl", arguments: []string{"get", "pods", "-o", "json"}, allowed: true},
		{name: "-o yaml is allowed", command: "kubectl", arguments: []string{"get", "pods", "-o", "yaml"}, allowed: true},
		{name: "-o kyaml is allowed", command: "kubectl", arguments: []string{"get", "pods", "-o", "kyaml"}, allowed: true},
		{name: "-o name is allowed", command: "kubectl", arguments: []string{"get", "pods", "-o", "name"}, allowed: true},
		{name: "-o wide is allowed", command: "kubectl", arguments: []string{"get", "pods", "-o", "wide"}, allowed: true},
		{name: "-o jsonpath is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath={.items[*].metadata.name}"}, allowed: true},
		{name: "-o jsonpath-as-json is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath-as-json={.items[*].metadata.name}"}, allowed: true},
		{name: "-o go-template is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template={{.metadata.name}}"}, allowed: true},
		{name: "-o template is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "template", "--template={{.metadata.name}}"}, allowed: true},
		{name: "-o custom-columns is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "custom-columns=NAME:.metadata.name"}, allowed: true},
		{name: "an output format is matched case-insensitively",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "YAML"}, allowed: true},
		{name: "a file output format is rejected case-insensitively",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "JSONPath-File=/etc/passwd"}, allowed: false,
			rejectionReason: outputFormatNotAllowed("jsonpath-file")},

		// Dash0 custom resources that can contain secrets: the formats that can reshape a value are rejected, because a
		// reshaped secret no longer contains its literal value and can therefore not be redacted.
		{name: "a Dash0 resource with -o go-template is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "go-template={{.spec}}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "a Dash0 resource with -o template is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "template", "--template={{.spec}}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "a Dash0 resource with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "jsonpath={.items[*].spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource with -o jsonpath-as-json is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "jsonpath-as-json={.items[*].spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath-as-json")},
		{name: "a Dash0 resource with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "custom-columns=T:.spec"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("custom-columns")},
		{name: "a Dash0 resource with --template but no output format is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "--template={{.spec}}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "a Dash0 resource with a truncating go-template is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", `go-template={{printf "%.6s" .spec.export.dash0.authorization.token}}`}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "a Dash0 resource with a comparing go-template is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", `go-template={{if lt .spec.export.dash0.authorization.token "m"}}A{{else}}B{{end}}`}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "a Dash0 resource with kyaml is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "kyaml"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("kyaml")},
		{name: "a Dash0 resource with an attached reshaping format is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-ojsonpath={.items}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource with a reshaping format in a grouped shorthand is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-Aojsonpath={.items}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource with a reshaping format overridden by an allowed one is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "jsonpath={.items}", "-o", "yaml"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource with an allowed format overridden by a reshaping one is rejected",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "yaml", "-o", "jsonpath={.items}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},

		// Every spelling of the resource type is covered, in every positional slot.
		{name: "the singular Dash0 resource type is covered",
			command: "kubectl", arguments: []string{"get", "dash0monitoring", "my-resource", "-o", "jsonpath={.spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "the kind of a Dash0 resource type is covered",
			command: "kubectl", arguments: []string{"get", "Dash0Monitoring", "-o", "jsonpath={.spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "the fully qualified Dash0 resource type is covered",
			command: "kubectl", arguments: []string{"get", "dash0monitorings.operator.dash0.com", "-o", "jsonpath={.spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource type in a comma-separated list is covered",
			command: "kubectl", arguments: []string{"get", "pods,dash0monitorings", "-o", "jsonpath={.items}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "a Dash0 resource type in a type/name pair in a later slot is covered",
			command: "kubectl", arguments: []string{"get", "pod/a", "dash0monitoring/b", "-o", "jsonpath={.spec}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "the operator configuration is covered",
			command: "kubectl", arguments: []string{"get", "dash0operatorconfigurations", "-o", "jsonpath={.items}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("jsonpath")},
		{name: "notification channels are covered",
			command: "kubectl", arguments: []string{"get", "dash0notificationchannels", "-o", "go-template={{.items}}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},
		{name: "synthetic checks are covered",
			command: "kubectl", arguments: []string{"get", "dash0syntheticchecks", "-o", "custom-columns=T:.spec"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("custom-columns")},
		{name: "describe of a Dash0 resource with --template is rejected",
			command: "kubectl", arguments: []string{"describe", "dash0monitorings", "--template={{.spec}}"}, allowed: false,
			rejectionReason: outputFormatNotRedactable("go-template")},

		// The formats the connector can redact stay available for Dash0 resources.
		{name: "a Dash0 resource with -o yaml is allowed",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "yaml"}, allowed: true},
		{name: "a Dash0 resource with -o json is allowed",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-A", "-o", "json"}, allowed: true},
		{name: "a Dash0 resource with -o name is allowed",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "name"}, allowed: true},
		{name: "a Dash0 resource with -o wide is allowed",
			command: "kubectl", arguments: []string{"get", "dash0monitorings", "-o", "wide"}, allowed: true},
		{name: "a Dash0 resource without an output format is allowed",
			command: "kubectl", arguments: []string{"get", "dash0monitorings"}, allowed: true},
		// describe renders a text format that cannot be parsed, so the credentials cannot be located in its output.
		{name: "describe of a Dash0 resource is rejected",
			command: "kubectl", arguments: []string{"describe", "dash0monitoring", "my-resource"}, allowed: false,
			rejectionReason: describeOfDash0ResourceNotSupported},
		{name: "describe of Dash0 resources in all namespaces is rejected",
			command: "kubectl", arguments: []string{"describe", "dash0monitorings", "-A"}, allowed: false,
			rejectionReason: describeOfDash0ResourceNotSupported},
		{name: "describe of a Dash0 resource via type/name is rejected",
			command: "kubectl", arguments: []string{"describe", "dash0notificationchannel/my-channel"}, allowed: false,
			rejectionReason: describeOfDash0ResourceNotSupported},
		{name: "describe of a Dash0 resource type without secrets is allowed",
			command: "kubectl", arguments: []string{"describe", "dash0views"}, allowed: true},
		{name: "describe of a non-Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"describe", "pod", "my-pod"}, allowed: true},
		{name: "events of a Dash0 resource are allowed",
			command: "kubectl", arguments: []string{"events", "--for", "dash0monitoring/my-resource"}, allowed: true},
		{name: "explain for a Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"explain", "dash0monitorings"}, allowed: true},

		// Resource types without secrets keep every generally allowed output format.
		{name: "a reshaping format for a non-Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath={.items[*].metadata.name}"}, allowed: true},
		{name: "a go-template for a non-Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template={{.items}}"}, allowed: true},
		{name: "--template for a non-Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "--template={{.items}}"}, allowed: true},
		{name: "kyaml for a non-Dash0 resource is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "kyaml"}, allowed: true},
		{name: "a Dash0 resource type without secrets keeps the reshaping formats",
			command: "kubectl", arguments: []string{"get", "dash0views", "-o", "jsonpath={.items}"}, allowed: true},

		// Secrets: listing and presence checks are allowed, reading their contents is not.
		{name: "listing secrets is allowed",
			command: "kubectl", arguments: []string{"get", "secrets"}, allowed: true},
		{name: "listing secrets in a namespace is allowed",
			command: "kubectl", arguments: []string{"get", "secrets", "-n", "x"}, allowed: true},
		{name: "presence check of a secret is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret"}, allowed: true},
		{name: "presence check via type/name is allowed",
			command: "kubectl", arguments: []string{"get", "secret/my-secret"}, allowed: true},
		{name: "describe secret is allowed",
			command: "kubectl", arguments: []string{"describe", "secret", "my-secret"}, allowed: true},
		{name: "secret with -o name is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "name"}, allowed: true},
		{name: "secret with -o wide is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "wide"}, allowed: true},

		{name: "secret with -o yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with -o json is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "json"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with -ojson (combined) is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-ojson"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with --output=yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--output=yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "jsonpath={.data}"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with -o go-template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "go-template={{.data}}"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "custom-columns=D:.data"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with --template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--template={{.data}}"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "listing secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret via type/name as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret/my-secret", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "fully qualified secret as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets.v1.", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "output flag before resource is rejected",
			command: "kubectl", arguments: []string{"get", "-o", "yaml", "secret", "my-secret"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "multi-resource list including secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret,pods", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret as yaml with a leading flag is rejected",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "presence check of a secret with a leading flag is allowed",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret"}, allowed: true},

		{name: "secret with the output format in a grouped shorthand is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-Aoyaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with an allowed output format overridden by yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "name", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with yaml overridden by an allowed output format is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "yaml", "-o", "name"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},
		{name: "secret with --template in front of the resource is rejected",
			command: "kubectl", arguments: []string{"get", "--template", "{{.data}}", "secret"}, allowed: false,
			rejectionReason: contentsNotReadable("secret")},

		// Config maps: listing and presence checks are allowed, reading their contents is not. Unlike `describe secret`,
		// `describe configmap` prints every value and is therefore rejected.
		{name: "listing config maps is allowed",
			command: "kubectl", arguments: []string{"get", "configmaps"}, allowed: true},
		{name: "listing config maps via the shortname is allowed",
			command: "kubectl", arguments: []string{"get", "cm", "-n", "x"}, allowed: true},
		{name: "presence check of a config map is allowed",
			command: "kubectl", arguments: []string{"get", "configmap", "my-cm"}, allowed: true},
		{name: "config map with -o name is allowed",
			command: "kubectl", arguments: []string{"get", "cm", "-o", "name"}, allowed: true},
		{name: "config map with -o wide is allowed",
			command: "kubectl", arguments: []string{"get", "cm", "-o", "wide"}, allowed: true},
		{name: "explain for config maps is allowed",
			command: "kubectl", arguments: []string{"explain", "cm"}, allowed: true},

		{name: "config map with -o yaml is rejected",
			command: "kubectl", arguments: []string{"get", "configmap", "my-cm", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map shortname with -o yaml is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "my-cm", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map via type/name as json is rejected",
			command: "kubectl", arguments: []string{"get", "cm/my-cm", "-o", "json"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "my-cm", "-o", "jsonpath={.data}"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-o", "custom-columns=D:.data"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map with --template is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "--template={{.data}}"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config map with an attached output format is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-oyaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "fully qualified config map as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "configmaps.v1.", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "config maps in all namespaces as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-A", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "multi-resource list including config maps as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "pods,cm", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "type/name pair for a config map in a later positional slot as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "pod/a", "cm/b", "-o", "yaml"}, allowed: false,
			rejectionReason: contentsNotReadable("config map")},
		{name: "describe configmap is rejected",
			command: "kubectl", arguments: []string{"describe", "configmap", "my-cm"}, allowed: false,
			rejectionReason: contentExposingSubcommand("describe", "config map")},
		{name: "describe cm is rejected",
			command: "kubectl", arguments: []string{"describe", "cm"}, allowed: false,
			rejectionReason: contentExposingSubcommand("describe", "config map")},
		{name: "describe config map via type/name is rejected",
			command: "kubectl", arguments: []string{"describe", "cm/my-cm"}, allowed: false,
			rejectionReason: contentExposingSubcommand("describe", "config map")},

		// Non-sensitive resources are unaffected by the content check.
		{name: "non-secret resource as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "yaml"}, allowed: true},
		{name: "pod named secret as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "secret", "-o", "yaml"}, allowed: true},
		{name: "namespace named secret as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-n", "secret", "-o", "yaml"}, allowed: true},
		{name: "label selector value named secret is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-l", "app=secret", "-o", "yaml"}, allowed: true},
		{name: "namespace named cm as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-n", "cm", "-o", "yaml"}, allowed: true},
		{name: "pod named cm as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "cm", "-o", "yaml"}, allowed: true},
		{name: "describe pod named cm is allowed",
			command: "kubectl", arguments: []string{"describe", "pod", "cm"}, allowed: true},

		// Allowlisted flags keep working, in every spelling and combination.
		{name: "all-namespaces and output shaping flags are allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-A", "--no-headers", "--show-labels", "-L", "app"}, allowed: true},
		{name: "selector, field selector and sort-by are allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-l", "app=x", "--field-selector", "status.phase=Running", "--sort-by", ".metadata.name"}, allowed: true},
		{name: "a value starting with a dash is not mistaken for a flag",
			command: "kubectl", arguments: []string{"logs", "my-pod", "--tail", "-1"}, allowed: true},
		{name: "logs flags are allowed",
			command: "kubectl", arguments: []string{"logs", "my-pod", "-c", "c1", "--since", "5m", "--previous", "--timestamps"}, allowed: true},
		{name: "grouped shorthands of allowed flags are allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-Ao", "wide"}, allowed: true},
		{name: "output format attached to a shorthand is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-owide"}, allowed: true},
		{name: "top and events flags are allowed",
			command: "kubectl", arguments: []string{"events", "--for", "pod/my-pod", "--types", "Warning"}, allowed: true},
		{name: "api-resources flags are allowed",
			command: "kubectl", arguments: []string{"api-resources", "--api-group", "apps", "--namespaced=false", "-o", "name"}, allowed: true},
		{name: "explain flags are allowed",
			command: "kubectl", arguments: []string{"explain", "pods", "--recursive", "--api-version", "v1"}, allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.CommandRequest{Command: tt.command, Arguments: tt.arguments}
			_, err := validateCommandAndParseArguments(req)

			if tt.allowed {
				if tt.rejectionReason != "" {
					t.Fatalf("invalid test case \"%s\": expects allowed, but also declares a rejection reason (\"%s\")", tt.name, tt.rejectionReason)
				}
				if err != nil {
					t.Errorf("expected request to be allowed, but it was rejected: %v", err)
				}
				return
			}

			if tt.rejectionReason == "" {
				t.Fatalf(
					"invalid test case \"%s\": expects the request to be rejected, but it does not declare the expected reason",
					tt.name,
				)
			}
			if err == nil {
				t.Fatal("expected request to be rejected, but it was allowed")
			}
			// The reason is compared, not only the fact that the request is rejected: otherwise a case can pass because
			// an earlier check rejected the request, and the restriction it means to cover is never exercised.
			if err.Error() != tt.rejectionReason {
				t.Errorf(
					"expected the request to be rejected with\n\t%s\nbut it was rejected with\n\t%s",
					tt.rejectionReason,
					err,
				)
			}
		})
	}
}

// subcommandRedactionRationale records, for every subcommand in allowedKubectlSubcommands, why its response cannot
// hand out a credential. Only "get" is routed through redaction (see responseCanContainSecrets, which returns false for
// every other subcommand), so every other entry has to justify itself by not rendering the content of a resource at
// all. Adding a subcommand to the allowlist without recording a rationale here fails
// TestEveryAllowedSubcommandHasARedactionRationale.
var subcommandRedactionRationale = map[string]string{
	"get": "the only subcommand whose response is redacted, see redactSecretsInResponse",
	"describe": "renders resource content, but is rejected for the resource types that can contain secrets, see " +
		"describeOfResourceTypeWithSecrets",
	"cluster-info": "the bare form only prints the addresses of the control plane and of the cluster's services; its " +
		"sub-verbs are rejected, see allowedSubVerbsPerSubCommand",
	"api-resources": "prints the known resource types and their metadata, never the content of an instance",
	"api-versions":  "prints the available API group/versions only",
	"explain":       "prints the schema of a resource type, not the content of an instance",
	"events": "renders Event objects; their messages are emitted by the kubelet and by controllers and do not " +
		"carry the credential fields of a Dash0 custom resource",
	"top": "prints a CPU/memory usage table only",
	"auth": "restricted to \"can-i\", see allowedSubVerbsPerSubCommand; it answers with yes/no or with the rule " +
		"list of the agent0-connector's own service account, never with the content of a resource",
	"version": "prints the client and server version only",
	"logs": "streams the raw log output of a container, which is not resource content; a credential a workload " +
		"logs itself is out of reach of response redaction",
}

// TestEveryAllowedSubcommandHasARedactionRationale guards against the drift that let "kubectl cluster-info dump" hand
// out the pod specs (and hence the Dash0 auth token in their env vars) of the whole cluster: the subcommand was on the
// allowlist, while redaction only ever ran for "get". Whenever allowedKubectlSubcommands grows, the new subcommand has
// to be classified deliberately.
func TestEveryAllowedSubcommandHasARedactionRationale(t *testing.T) {
	for subcommand := range allowedKubectlSubcommands {
		if _, hasRationale := subcommandRedactionRationale[subcommand]; !hasRationale {
			t.Errorf(
				"the kubectl subcommand %q is allowed, but no rationale records why its response cannot expose a "+
					"credential; redaction only runs for \"get\" (see responseCanContainSecrets), so either confirm that "+
					"this subcommand cannot render resource content and add a rationale to "+
					"subcommandRedactionRationale, or restrict it in validation.go",
				subcommand,
			)
		}
	}
	for subcommand := range subcommandRedactionRationale {
		if _, allowed := allowedKubectlSubcommands[subcommand]; !allowed {
			t.Errorf(
				"subcommandRedactionRationale has a stale entry for %q, which is not an allowed subcommand any more",
				subcommand,
			)
		}
	}
}

// TestOnlyGetIsRoutedThroughRedaction pins the invariant the rationales above rely on: responseCanContainSecrets
// redacts the response of "get" only. A subcommand that starts rendering resource content therefore leaks it unless it
// is restricted in validation.go.
func TestOnlyGetIsRoutedThroughRedaction(t *testing.T) {
	for subcommand := range allowedKubectlSubcommands {
		parsed := parseArguments([]string{subcommand, "dash0monitorings", "-o", "yaml"})
		canContainSecrets := responseCanContainSecrets(parsed)
		if subcommand == "get" && !canContainSecrets {
			t.Errorf("expected the response of %q to be routed through redaction", subcommand)
		}
		if subcommand != "get" && canContainSecrets {
			t.Errorf(
				"the response of %q is now routed through redaction; update subcommandRedactionRationale, which "+
					"records that only \"get\" is",
				subcommand,
			)
		}
	}
}

func TestLookupSensitiveResourceType(t *testing.T) {
	tests := []struct {
		resourceType string
		isSensitive  bool
		displayName  string
	}{
		{resourceType: "secret", isSensitive: true, displayName: "secret"},
		{resourceType: "secrets", isSensitive: true, displayName: "secret"},
		{resourceType: "Secrets", isSensitive: true, displayName: "secret"},
		{resourceType: "secrets.v1.", isSensitive: true, displayName: "secret"},
		{resourceType: "configmap", isSensitive: true, displayName: "config map"},
		{resourceType: "configmaps", isSensitive: true, displayName: "config map"},
		{resourceType: "cm", isSensitive: true, displayName: "config map"},
		{resourceType: "CM", isSensitive: true, displayName: "config map"},
		{resourceType: "configmaps.v1.", isSensitive: true, displayName: "config map"},
		{resourceType: "cm.v1.", isSensitive: true, displayName: "config map"},
		{resourceType: "pods", isSensitive: false},
		{resourceType: "sealedsecrets", isSensitive: false},
		{resourceType: "", isSensitive: false},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			resource, isSensitive := lookupSensitiveResourceType(tt.resourceType)
			if isSensitive != tt.isSensitive {
				t.Fatalf("expected isSensitive=%t, got %t", tt.isSensitive, isSensitive)
			}
			if resource.displayName != tt.displayName {
				t.Errorf("expected display name %q, got %q", tt.displayName, resource.displayName)
			}
		})
	}
}
