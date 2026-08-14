// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"testing"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

//nolint:lll
func TestValidateCommandRequest(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		arguments []string
		allowed   bool
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
		{name: "top is allowed", command: "kubectl", arguments: []string{"top", "pods"}, allowed: true},
		{name: "events is allowed", command: "kubectl", arguments: []string{"events"}, allowed: true},

		{name: "non-kubectl command is rejected", command: "helm", arguments: []string{"list"}, allowed: false},
		{name: "empty command is rejected", command: "", arguments: []string{"get", "pods"}, allowed: false},
		{name: "mutating delete is rejected", command: "kubectl", arguments: []string{"delete", "pod", "x"}, allowed: false},
		{name: "mutating apply is rejected", command: "kubectl", arguments: []string{"apply", "-f", "x"}, allowed: false},
		{name: "mutating edit is rejected", command: "kubectl", arguments: []string{"edit", "deploy", "x"}, allowed: false},
		{name: "leading value-taking flag before subcommand is allowed", command: "kubectl", arguments: []string{"-n", "x", "get", "po"}, allowed: true},
		{name: "leading flag in --flag=value form before subcommand is allowed", command: "kubectl", arguments: []string{"--namespace=x", "get", "po"}, allowed: true},
		{name: "leading boolean flag before subcommand is allowed", command: "kubectl", arguments: []string{"--no-headers=true", "get", "po"}, allowed: true},
		{name: "leading flags before a mutating subcommand are still rejected", command: "kubectl", arguments: []string{"-n", "x", "delete", "pod", "y"}, allowed: false},
		{name: "sensitive flag before subcommand is still rejected", command: "kubectl", arguments: []string{"--kubeconfig=/x", "get", "po"}, allowed: false},
		{name: "watch flag (-w) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-w"}, allowed: false},
		{name: "--watch is rejected", command: "kubectl", arguments: []string{"get", "po", "--watch"}, allowed: false},
		{name: "--watch-only is rejected", command: "kubectl", arguments: []string{"get", "pods", "--watch-only"}, allowed: false},
		{name: "--watch=true is rejected", command: "kubectl", arguments: []string{"get", "--watch=true"}, allowed: false},
		{name: "--watch-only=true is rejected", command: "kubectl", arguments: []string{"get", "--watch-only=true"}, allowed: false},

		{name: "follow flag (-f) is rejected", command: "kubectl", arguments: []string{"logs", "x", "-f"}, allowed: false},
		{name: "--follow is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow"}, allowed: false},
		{name: "--follow=true is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow=true"}, allowed: false},

		// Flags outside the allowlist are rejected, whatever they are for; --raw would otherwise turn "get" into an
		// arbitrary API request, bypassing the resource-based secret check below.
		{name: "--raw is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/secrets/my-secret"}, allowed: false},
		{name: "--raw is rejected for any type", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/pods/my-pod"}, allowed: false},
		{name: "--raw=value is rejected", command: "kubectl", arguments: []string{"get", "--raw=/api/v1/namespaces/default/secrets/my-secret"}, allowed: false},
		{name: "--raw for a cluster-wide collection is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/secrets"}, allowed: false},
		{name: "--raw for a configmap is rejected", command: "kubectl", arguments: []string{"get", "--raw", "/api/v1/namespaces/default/configmaps/my-cm"}, allowed: false},
		{name: "an unknown flag is rejected", command: "kubectl", arguments: []string{"get", "pods", "--show-managed-fields-typo"}, allowed: false},
		// -v=8 and above make kubectl log the HTTP response bodies, exposing the contents of any resource on stderr.
		{name: "verbosity flag is rejected", command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-v=9"}, allowed: false},
		{name: "verbosity flag with a separate value is rejected", command: "kubectl", arguments: []string{"get", "cm", "-v", "8"}, allowed: false},
		{name: "long verbosity flag is rejected", command: "kubectl", arguments: []string{"get", "pods", "--v=9"}, allowed: false},
		{name: "a grouped shorthand with an unknown member is rejected", command: "kubectl", arguments: []string{"get", "pods", "-Az"}, allowed: false},
		{name: "--filename is rejected", command: "kubectl", arguments: []string{"get", "--filename", "pod.yaml"}, allowed: false},
		{name: "--kustomize is rejected", command: "kubectl", arguments: []string{"get", "-k", "dir"}, allowed: false},
		{name: "the end-of-flags separator is rejected", command: "kubectl", arguments: []string{"get", "pods", "--"}, allowed: false},
		{name: "a bare dash is rejected", command: "kubectl", arguments: []string{"get", "pods", "-"}, allowed: false},

		{name: "impersonation (--as) is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as", "system:admin"}, allowed: false},
		{name: "--as-group is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as-group=system:masters"}, allowed: false},
		{name: "--server is rejected", command: "kubectl", arguments: []string{"get", "pods", "--server", "https://evil"}, allowed: false},
		{name: "-s (server short flag) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-s", "https://evil"}, allowed: false},
		{name: "--kubeconfig is rejected", command: "kubectl", arguments: []string{"get", "pods", "--kubeconfig=/x"}, allowed: false},
		{name: "--context is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context", "other"}, allowed: false},
		{name: "--context=value is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context=other"}, allowed: false},
		{name: "--token is rejected", command: "kubectl", arguments: []string{"get", "pods", "--token", "abc"}, allowed: false},
		{name: "--insecure-skip-tls-verify is rejected", command: "kubectl", arguments: []string{"get", "pods", "--insecure-skip-tls-verify"}, allowed: false},

		// Output formats: the value of -o/--output is checked against an allowlist. The formats that take their
		// template from a file render that file, so they would read an arbitrary file from the connector's own
		// container; they are rejected for every resource type, sensitive or not.
		{name: "-o go-template-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file=/etc/passwd"}, allowed: false},
		{name: "-o go-template-file with an attached value is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-ogo-template-file=/etc/passwd"}, allowed: false},
		{name: "-o go-template-file taking its path from --template is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file", "--template", "/etc/passwd"}, allowed: false},
		{name: "-o templatefile is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "templatefile", "--template=/etc/passwd"}, allowed: false},
		{name: "-o jsonpath-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath-file=/etc/passwd"}, allowed: false},
		{name: "--output=jsonpath-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "--output=jsonpath-file=/etc/passwd"}, allowed: false},
		{name: "-o custom-columns-file is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "custom-columns-file=/etc/passwd"}, allowed: false},
		{name: "a file output format in a grouped shorthand is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-Aocustom-columns-file=/etc/passwd"}, allowed: false},
		{name: "a file output format reading the service account token is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "go-template-file=/var/run/secrets/kubernetes.io/serviceaccount/token"}, allowed: false},
		{name: "a file output format is rejected even when overridden by an allowed one",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "jsonpath-file=/etc/passwd", "-o", "name"}, allowed: false},
		{name: "a file output format is rejected even when it overrides an allowed one",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "name", "-o", "jsonpath-file=/etc/passwd"}, allowed: false},
		{name: "an unknown output format is rejected",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "bogusformat"}, allowed: false},

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
			command: "kubectl", arguments: []string{"get", "pods", "-o", "JSONPath-File=/etc/passwd"}, allowed: false},

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
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "yaml"}, allowed: false},
		{name: "secret with -o json is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "json"}, allowed: false},
		{name: "secret with -ojson (combined) is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-ojson"}, allowed: false},
		{name: "secret with --output=yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--output=yaml"}, allowed: false},
		{name: "secret with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "jsonpath={.data}"}, allowed: false},
		{name: "secret with -o go-template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "go-template={{.data}}"}, allowed: false},
		{name: "secret with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "custom-columns=D:.data"}, allowed: false},
		{name: "secret with --template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--template={{.data}}"}, allowed: false},
		{name: "listing secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets", "-o", "yaml"}, allowed: false},
		{name: "secret via type/name as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret/my-secret", "-o", "yaml"}, allowed: false},
		{name: "fully qualified secret as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets.v1.", "-o", "yaml"}, allowed: false},
		{name: "output flag before resource is rejected",
			command: "kubectl", arguments: []string{"get", "-o", "yaml", "secret", "my-secret"}, allowed: false},
		{name: "multi-resource list including secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret,pods", "-o", "yaml"}, allowed: false},
		{name: "secret as yaml with a leading flag is rejected",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret", "-o", "yaml"}, allowed: false},
		{name: "presence check of a secret with a leading flag is allowed",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret"}, allowed: true},

		{name: "secret with the output format in a grouped shorthand is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-Aoyaml"}, allowed: false},
		{name: "secret with an allowed output format overridden by yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "name", "-o", "yaml"}, allowed: false},
		{name: "secret with yaml overridden by an allowed output format is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "yaml", "-o", "name"}, allowed: false},
		{name: "secret with --template in front of the resource is rejected",
			command: "kubectl", arguments: []string{"get", "--template", "{{.data}}", "secret"}, allowed: false},

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
			command: "kubectl", arguments: []string{"get", "configmap", "my-cm", "-o", "yaml"}, allowed: false},
		{name: "config map shortname with -o yaml is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "my-cm", "-o", "yaml"}, allowed: false},
		{name: "config map via type/name as json is rejected",
			command: "kubectl", arguments: []string{"get", "cm/my-cm", "-o", "json"}, allowed: false},
		{name: "config map with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "my-cm", "-o", "jsonpath={.data}"}, allowed: false},
		{name: "config map with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-o", "custom-columns=D:.data"}, allowed: false},
		{name: "config map with --template is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "--template={{.data}}"}, allowed: false},
		{name: "config map with an attached output format is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-oyaml"}, allowed: false},
		{name: "fully qualified config map as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "configmaps.v1.", "-o", "yaml"}, allowed: false},
		{name: "config maps in all namespaces as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "cm", "-A", "-o", "yaml"}, allowed: false},
		{name: "multi-resource list including config maps as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "pods,cm", "-o", "yaml"}, allowed: false},
		{name: "type/name pair for a config map in a later positional slot as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "pod/a", "cm/b", "-o", "yaml"}, allowed: false},
		{name: "describe configmap is rejected",
			command: "kubectl", arguments: []string{"describe", "configmap", "my-cm"}, allowed: false},
		{name: "describe cm is rejected",
			command: "kubectl", arguments: []string{"describe", "cm"}, allowed: false},
		{name: "describe config map via type/name is rejected",
			command: "kubectl", arguments: []string{"describe", "cm/my-cm"}, allowed: false},

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
			if tt.allowed && err != nil {
				t.Errorf("expected request to be allowed, but it was rejected: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected request to be rejected, but it was allowed")
			}
		})
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
