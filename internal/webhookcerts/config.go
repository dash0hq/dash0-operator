// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"fmt"
	"strings"
)

// InjectionTargetLabel marks the webhook configurations and CRDs into which the operator injects its CA bundle. The
// label value is the namespace of the operator installation.
const InjectionTargetLabel = "dash0.com/inject-ca-bundle"

// TargetKind is the kind of object into which the CA bundle is injected.
type TargetKind string

const (
	TargetKindMutatingWebhookConfiguration   TargetKind = "MutatingWebhookConfiguration"
	TargetKindValidatingWebhookConfiguration TargetKind = "ValidatingWebhookConfiguration"
	TargetKindCustomResourceDefinition       TargetKind = "CustomResourceDefinition"
)

// Target is an object into which the CA bundle is injected.
type Target struct {
	Kind TargetKind
	Name string
}

func (t Target) String() string {
	return fmt.Sprintf("%s/%s", t.Kind, t.Name)
}

// Config holds the settings for the operator-managed webhook certificates.
type Config struct {
	OperatorNamespace string
	// SecretName is the name of the Secret (rendered empty by the Helm chart) in which the material is stored.
	SecretName string
	// PreviousSecretName is the name of the Secret in which older chart versions stored their certificates. Its CA is
	// retained in the bundle. Optional.
	PreviousSecretName string
	DeploymentName     string
	WebhookServiceName string
	// Targets are the only objects that are read, retained from, or patched; the injection label alone is not enough.
	Targets []Target
	// PersesCrdName is the name of the Perses dashboard CRD, if the operator patches its conversion webhook. A pending
	// CA only becomes active once that CRD carries the current bundle as well. Optional.
	PersesCrdName string
}

// ParseTargets parses a comma-separated list of Kind/Name pairs, as provided by the Helm chart.
func ParseTargets(raw string) ([]Target, error) {
	var targets []Target
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kind, name, found := strings.Cut(entry, "/")
		if !found || name == "" {
			return nil, fmt.Errorf("invalid injection target %q, expected Kind/Name", entry)
		}
		switch TargetKind(kind) {
		case TargetKindMutatingWebhookConfiguration,
			TargetKindValidatingWebhookConfiguration,
			TargetKindCustomResourceDefinition:
			targets = append(targets, Target{Kind: TargetKind(kind), Name: name})
		default:
			return nil, fmt.Errorf("unsupported kind in injection target %q", entry)
		}
	}
	return targets, nil
}
