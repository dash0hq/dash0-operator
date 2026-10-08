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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
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

	It("finds no unexpected drift between Dash0View and the API client's view type", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[dash0v1alpha1.Dash0ViewSpec](),
			reflect.TypeFor[dash0apiclient.ViewSpec](),
			map[string]string{
				"display.folder": "ignored by the Dash0 API, folders are set via the dash0.com/folder-path annotation",
			},
			map[string]string{
				"query":                "not supported by Dash0View yet",
				"serviceMapProperties": "not supported by Dash0View yet",
				"serviceName":          "not supported by Dash0View yet",
			},
		)
	})
	syntheticCheckUnionReason := "a union in the API client which keeps the raw JSON, see synthetic_check_definition_roundtrip_test.go"

	It("finds no unexpected drift between Dash0SyntheticCheck and the API client's synthetic check type", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[dash0v1alpha1.Dash0SyntheticCheckSpec](),
			reflect.TypeFor[dash0apiclient.SyntheticCheckSpec](),
			map[string]string{
				"plugin.kind":  syntheticCheckUnionReason,
				"plugin.spec":  syntheticCheckUnionReason,
				"retries.kind": syntheticCheckUnionReason,
				"retries.spec": syntheticCheckUnionReason,
			},
			map[string]string{
				"notifications.onlyCriticalChannels": "not supported by Dash0SyntheticCheck yet",
				"permissions":                        "not supported by Dash0SyntheticCheck yet",
			},
		)
	})

	It("finds no unexpected drift between the synthetic check plugin and the API client's HTTP check plugin", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[dash0v1alpha1.Dash0SyntheticCheckPlugin](),
			reflect.TypeFor[dash0apiclient.SyntheticHttpCheckPlugin](),
			map[string]string{
				"spec.assertions.criticalAssertions.kind": syntheticCheckUnionReason,
				"spec.assertions.criticalAssertions.spec": syntheticCheckUnionReason,
				"spec.assertions.degradedAssertions.kind": syntheticCheckUnionReason,
				"spec.assertions.degradedAssertions.spec": syntheticCheckUnionReason,
			},
			nil,
		)
	})

	It("finds no unexpected drift between the synthetic check retries and the API client's retries types", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[dash0v1alpha1.Dash0SyntheticCheckRetries](),
			reflect.TypeFor[dash0apiclient.SyntheticCheckRetriesExponential](),
			nil,
			nil,
		)
	})
	samplingConditionUnionReason := "a union in the API client which keeps the raw JSON, see sampling_rule_definition_roundtrip_test.go"
	samplingConditionVariantReason := "the custom resource has one condition spec type for all condition kinds"

	It("finds no unexpected drift between Dash0SamplingRule and the API client's sampling rule type", func() {
		expectNoUnexpectedApiTypeDrift(
			reflect.TypeFor[dash0v1alpha1.Dash0SamplingRuleSpec](),
			reflect.TypeFor[dash0apiclient.SamplingSpec](),
			map[string]string{
				"conditions.kind": samplingConditionUnionReason,
				"conditions.spec": samplingConditionUnionReason,
			},
			nil,
		)
	})

	DescribeTable("finds no unexpected drift between the sampling rule condition and the API client's condition types",
		func(clientType reflect.Type, allowedOnlyInCrd map[string]string) {
			expectNoUnexpectedApiTypeDrift(
				reflect.TypeFor[dash0v1alpha1.Dash0SamplingRuleCondition](),
				clientType,
				allowedOnlyInCrd,
				nil,
			)
		},
		Entry("probabilistic", reflect.TypeFor[dash0apiclient.SamplingConditionProbabilistic](), map[string]string{
			"spec.ottl":       samplingConditionVariantReason,
			"spec.conditions": samplingConditionVariantReason,
		}),
		Entry("ottl", reflect.TypeFor[dash0apiclient.SamplingConditionOttl](), map[string]string{
			"spec.rate":       samplingConditionVariantReason,
			"spec.conditions": samplingConditionVariantReason,
		}),
		Entry("and", reflect.TypeFor[dash0apiclient.SamplingConditionAnd](), map[string]string{
			"spec.rate": samplingConditionVariantReason,
			"spec.ottl": samplingConditionVariantReason,
		}),
	)

	It("finds no unexpected drift between Dash0NotificationChannel and the API client's notification channel type",
		func() {
			movedToConfig := "assembled into spec.config by prepareNotificationChannelApiPayload"
			expectNoUnexpectedApiTypeDrift(
				reflect.TypeFor[dash0v1beta1.Dash0NotificationChannelSpec](),
				reflect.TypeFor[dash0apiclient.NotificationChannelSpec](),
				map[string]string{
					"display":                 "moved to metadata.name by prepareNotificationChannelApiPayload",
					"slackConfig":             movedToConfig,
					"slackBotConfig":          movedToConfig,
					"emailV2Config":           movedToConfig,
					"webhookConfig":           movedToConfig,
					"incidentioConfig":        movedToConfig,
					"opsgenieConfig":          movedToConfig,
					"pagerdutyConfig":         movedToConfig,
					"teamsWebhookConfig":      movedToConfig,
					"discordWebhookConfig":    movedToConfig,
					"googleChatWebhookConfig": movedToConfig,
					"ilertConfig":             movedToConfig,
					"allQuietConfig":          movedToConfig,
				},
				map[string]string{
					"config": "assembled from the type-specific *Config field by prepareNotificationChannelApiPayload",
				},
			)
		})

	DescribeTable("finds no drift between the notification channel config types",
		func(crdType reflect.Type, clientType reflect.Type) {
			expectNoUnexpectedApiTypeDrift(crdType, clientType, nil, nil)
		},
		Entry(
			"slack",
			reflect.TypeFor[dash0v1beta1.SlackConfig](),
			reflect.TypeFor[dash0apiclient.SlackConfig](),
		),
		Entry(
			"slack_bot",
			reflect.TypeFor[dash0v1beta1.SlackBotConfig](),
			reflect.TypeFor[dash0apiclient.SlackBotConfig](),
		),
		Entry(
			"email_v2",
			reflect.TypeFor[dash0v1beta1.EmailV2Config](),
			reflect.TypeFor[dash0apiclient.EmailV2Config](),
		),
		Entry(
			"webhook",
			reflect.TypeFor[dash0v1beta1.WebhookConfig](),
			reflect.TypeFor[dash0apiclient.WebhookConfig](),
		),
		Entry(
			"incidentio",
			reflect.TypeFor[dash0v1beta1.IncidentioConfig](),
			reflect.TypeFor[dash0apiclient.IncidentIOConfig](),
		),
		Entry(
			"opsgenie",
			reflect.TypeFor[dash0v1beta1.OpsgenieConfig](),
			reflect.TypeFor[dash0apiclient.OpsgenieConfig](),
		),
		Entry(
			"pagerduty",
			reflect.TypeFor[dash0v1beta1.PagerdutyConfig](),
			reflect.TypeFor[dash0apiclient.PagerDutyConfig](),
		),
		Entry(
			"teams_webhook",
			reflect.TypeFor[dash0v1beta1.TeamsWebhookConfig](),
			reflect.TypeFor[dash0apiclient.TeamsWebhookConfig](),
		),
		Entry(
			"discord_webhook",
			reflect.TypeFor[dash0v1beta1.DiscordWebhookConfig](),
			reflect.TypeFor[dash0apiclient.DiscordWebhookConfig](),
		),
		Entry(
			"google_chat_webhook",
			reflect.TypeFor[dash0v1beta1.GoogleChatWebhookConfig](),
			reflect.TypeFor[dash0apiclient.GoogleChatWebhookConfig](),
		),
		Entry(
			"ilert",
			reflect.TypeFor[dash0v1beta1.IlertConfig](),
			reflect.TypeFor[dash0apiclient.IlertConfig](),
		),
		Entry(
			"all_quiet",
			reflect.TypeFor[dash0v1beta1.AllQuietConfig](),
			reflect.TypeFor[dash0apiclient.AllQuietConfig](),
		),
	)
})
