// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"reflect"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
)

type namedCrdSchemaDriftCheck struct {
	name  string
	check crdSchemaDriftCheck
}

const (
	attributeFilterValueReason = "the custom resources represent attribute values as strings, the API client " +
		"accepts an AnyValue object as well"
	notSupportedByDash0ViewYet           = "not supported by Dash0View yet"
	notSupportedByDash0SyntheticCheckYet = "not supported by Dash0SyntheticCheck yet"
	onlyHttpSyntheticChecksReason        = "the custom resource only supports http checks"
	errorAssertionValueReason            = "the custom resource has one value field for all assertion kinds, the " +
		"values of error assertions are restricted by a CEL validation rule"
	movedToConfigReason        = "assembled into spec.config by prepareNotificationChannelApiPayload"
	sloMetricSourceSpecReason  = "an untyped map in the API client, see slo_definition_roundtrip_test.go"
	sloNotSupportedByApiReason = "accepted for OpenSLO compatibility, but ignored or rejected by the Dash0 API"
	sloThresholdReason         = "threshold SLOs, which the API client documents as not supported yet"
	sloOnlyRatioMetricReason   = "the custom resource only supports an inline ratioMetric indicator, the only " +
		"kind of indicator the API client documents as supported"
	sloPrometheusOnlyReason = "the API client documents Prometheus as the only supported metric source"
	sloTimeWindowReason     = "the API client documents a rolling 28d (4w) window as the only supported time window"
)

var namedCrdSchemaDriftChecks = []namedCrdSchemaDriftCheck{
	{"Dash0View", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0views.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.ViewSpec](),
		allowedDifferences: map[string]string{
			"only in CRD: display.folder": "ignored by the Dash0 API, folders are set via the dash0.com/folder-path " +
				"annotation",
			"only in client: frameFilters":                                          notSupportedByDash0ViewYet,
			"only in client: invertCallStack":                                       notSupportedByDash0ViewYet,
			"only in client: profileType":                                           notSupportedByDash0ViewYet,
			"only in client: query":                                                 notSupportedByDash0ViewYet,
			"only in client: serviceMapProperties":                                  notSupportedByDash0ViewYet,
			"only in client: serviceName":                                           notSupportedByDash0ViewYet,
			"only in client: stackFilters":                                          notSupportedByDash0ViewYet,
			"only in client: sumBy":                                                 notSupportedByDash0ViewYet,
			"type: clientFilter.value: string in CRD, object|string in client":      attributeFilterValueReason,
			"type: clientFilter.values[]: string in CRD, object|string in client":   attributeFilterValueReason,
			"type: filter.value: string in CRD, object|string in client":            attributeFilterValueReason,
			"type: filter.values[]: string in CRD, object|string in client":         attributeFilterValueReason,
			"type: implicitFilter.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: implicitFilter.values[]: string in CRD, object|string in client": attributeFilterValueReason,
		},
	}},
	{"Dash0SyntheticCheck", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0syntheticchecks.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.SyntheticCheckSpec](),
		allowedDifferences: map[string]string{
			"only in client: annotations":                        notSupportedByDash0SyntheticCheckYet,
			"only in client: notifications.onlyCriticalChannels": notSupportedByDash0SyntheticCheckYet,
			"only in client: permissions":                        notSupportedByDash0SyntheticCheckYet,
			"required only in CRD: display": "stricter in the custom resource, every synthetic check managed by the " +
				"operator has a name",
			"enum only in some union variants: plugin.spec.assertions.criticalAssertions.spec.value": errorAssertionValueReason,
			"enum only in some union variants: plugin.spec.assertions.degradedAssertions.spec.value": errorAssertionValueReason,
			"enum value only in client: plugin.kind = browser":                                       onlyHttpSyntheticChecksReason,
			"only in client: plugin.spec.env":                                                        onlyHttpSyntheticChecksReason,
			"only in client: plugin.spec.script":                                                     onlyHttpSyntheticChecksReason,
			"required only in CRD: plugin.spec.assertions":                                           onlyHttpSyntheticChecksReason,
			"required only in CRD: plugin.spec.request":                                              onlyHttpSyntheticChecksReason,
		},
	}},
	{"Dash0NotificationChannel", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0notificationchannels.yaml",
		version:    "v1beta1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.NotificationChannelSpec](),
		allowedDifferences: map[string]string{
			"only in CRD: display":                 "moved to metadata.name by prepareNotificationChannelApiPayload",
			"only in CRD: slackConfig":             movedToConfigReason,
			"only in CRD: slackBotConfig":          movedToConfigReason,
			"only in CRD: emailV2Config":           movedToConfigReason,
			"only in CRD: webhookConfig":           movedToConfigReason,
			"only in CRD: incidentioConfig":        movedToConfigReason,
			"only in CRD: opsgenieConfig":          movedToConfigReason,
			"only in CRD: pagerdutyConfig":         movedToConfigReason,
			"only in CRD: teamsWebhookConfig":      movedToConfigReason,
			"only in CRD: discordWebhookConfig":    movedToConfigReason,
			"only in CRD: googleChatWebhookConfig": movedToConfigReason,
			"only in CRD: ilertConfig":             movedToConfigReason,
			"only in CRD: allQuietConfig":          movedToConfigReason,
			"only in CRD: mattermostWebhookConfig": movedToConfigReason,
			"only in client: config": "assembled from the type-specific *Config field by " +
				"prepareNotificationChannelApiPayload, the config types are compared one by one",
			"type: routing.filters.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: routing.filters.values[]: string in CRD, object|string in client": attributeFilterValueReason,
		},
	}},
	notificationChannelConfigCheck("slackConfig", reflect.TypeFor[dash0apiclient.SlackConfig]()),
	notificationChannelConfigCheck("slackBotConfig", reflect.TypeFor[dash0apiclient.SlackBotConfig]()),
	notificationChannelConfigCheck("emailV2Config", reflect.TypeFor[dash0apiclient.EmailV2Config]()),
	notificationChannelConfigCheck("webhookConfig", reflect.TypeFor[dash0apiclient.WebhookConfig]()),
	notificationChannelConfigCheck("incidentioConfig", reflect.TypeFor[dash0apiclient.IncidentIOConfig]()),
	notificationChannelConfigCheck("opsgenieConfig", reflect.TypeFor[dash0apiclient.OpsgenieConfig]()),
	notificationChannelConfigCheck("pagerdutyConfig", reflect.TypeFor[dash0apiclient.PagerDutyConfig]()),
	notificationChannelConfigCheck("teamsWebhookConfig", reflect.TypeFor[dash0apiclient.TeamsWebhookConfig]()),
	notificationChannelConfigCheck("discordWebhookConfig", reflect.TypeFor[dash0apiclient.DiscordWebhookConfig]()),
	notificationChannelConfigCheck("googleChatWebhookConfig",
		reflect.TypeFor[dash0apiclient.GoogleChatWebhookConfig]()),
	notificationChannelConfigCheck("ilertConfig", reflect.TypeFor[dash0apiclient.IlertConfig]()),
	notificationChannelConfigCheck("allQuietConfig", reflect.TypeFor[dash0apiclient.AllQuietConfig]()),
	notificationChannelConfigCheck("mattermostWebhookConfig",
		reflect.TypeFor[dash0apiclient.MattermostWebhookConfig]()),
	{"Dash0SamplingRule", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0samplingrules.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.SamplingSpec](),
		allowedDifferences: map[string]string{
			"required only in client: conditions.spec": "the custom resource does not require a spec for error " +
				"conditions, which have no settings; transformToSamplingApiPayload adds an empty spec",
			"type: conditions.spec.rate: string in CRD, number in client": "custom resources avoid floating point " +
				"numbers, the rate is a string with a validation pattern",
		},
	}},
	{"Dash0SpamFilter", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0spamfilters.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.SpamFilterSpec](),
		allowedDifferences: map[string]string{
			"type: filter.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: filter.values[]: string in CRD, object|string in client": attributeFilterValueReason,
		},
	}},
	{"Dash0SignalToMetrics", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0signaltometrics.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.SignalToMetricsSpec](),
		allowedDifferences: map[string]string{
			"only in client: alternativeDatasetId":                                                 "not supported by Dash0SignalToMetrics yet",
			"only in client: targetDatasetMode":                                                    "not supported by Dash0SignalToMetrics yet",
			"type: match.filters.value: string in CRD, object|string in client":                    attributeFilterValueReason,
			"type: match.filters.values[]: string in CRD, object|string in client":                 attributeFilterValueReason,
			"type: output.keepResourceAttributes.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: output.keepResourceAttributes.values[]: string in CRD, object|string in client": attributeFilterValueReason,
			"type: output.keepSignalAttributes.value: string in CRD, object|string in client":      attributeFilterValueReason,
			"type: output.keepSignalAttributes.values[]: string in CRD, object|string in client":   attributeFilterValueReason,
		},
	}},
	{"Dash0TimeSeriesAggregation", crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0timeseriesaggregations.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.TimeSeriesAggregationSpec](),
		allowedDifferences: map[string]string{
			"type: attributeModifications.spec.keyMatcher.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: attributeModifications.spec.keyMatcher.values[]: string in CRD, object|string in client": attributeFilterValueReason,
			"type: match.metricNameMatcher.value: string in CRD, object|string in client":                   attributeFilterValueReason,
			"type: match.metricNameMatcher.values[]: string in CRD, object|string in client":                attributeFilterValueReason,
			"type: match.otherFilters.value: string in CRD, object|string in client":                        attributeFilterValueReason,
			"type: match.otherFilters.values[]: string in CRD, object|string in client":                     attributeFilterValueReason,
		},
	}},
	{"Dash0GenerativeAiEvaluation", crdSchemaDriftCheck{
		crdFile:    "dash0.com_dash0generativeaievaluations.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.GenerativeAiEvaluationSpec](),
		allowedDifferences: map[string]string{
			"type: match.filters.value: string in CRD, object|string in client":    attributeFilterValueReason,
			"type: match.filters.values[]: string in CRD, object|string in client": attributeFilterValueReason,
		},
	}},
	{"Dash0Team", crdSchemaDriftCheck{
		crdFile:    "dash0.com_dash0teams.yaml",
		version:    "v1alpha1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.TeamSpec](),
	}},
	{"SLO", crdSchemaDriftCheck{
		crdFile:    "openslo.com_slos.yaml",
		version:    "v1",
		schemaPath: "spec",
		clientType: reflect.TypeFor[dash0apiclient.SloSpec](),
		allowedDifferences: map[string]string{
			"only in CRD: indicator.spec.ratioMetric.good.metricSource.spec.query":          sloMetricSourceSpecReason,
			"only in CRD: indicator.spec.ratioMetric.bad.metricSource.spec.query":           sloMetricSourceSpecReason,
			"only in CRD: indicator.spec.ratioMetric.total.metricSource.spec.query":         sloMetricSourceSpecReason,
			"only in CRD: indicator.spec.ratioMetric.raw.metricSource.spec.query":           sloMetricSourceSpecReason,
			"only in client: alertPolicies":                                                 sloNotSupportedByApiReason,
			"only in client: indicatorRef":                                                  sloNotSupportedByApiReason,
			"only in client: indicator.spec.ratioMetric.good.metricSource.metricSourceRef":  sloNotSupportedByApiReason,
			"only in client: indicator.spec.ratioMetric.bad.metricSource.metricSourceRef":   sloNotSupportedByApiReason,
			"only in client: indicator.spec.ratioMetric.total.metricSource.metricSourceRef": sloNotSupportedByApiReason,
			"only in client: indicator.spec.ratioMetric.raw.metricSource.metricSourceRef":   sloNotSupportedByApiReason,
			"only in client: objectives.compositeWeight":                                    sloNotSupportedByApiReason,
			"only in client: objectives.indicator":                                          sloNotSupportedByApiReason,
			"only in client: objectives.indicatorRef":                                       sloNotSupportedByApiReason,
			"only in client: timeWindow.calendar":                                           sloNotSupportedByApiReason,
			"enum value only in client: budgetingMethod = RatioTimeslices":                  sloNotSupportedByApiReason,
			"only in client: indicator.spec.thresholdMetric":                                sloThresholdReason,
			"only in client: objectives.op":                                                 sloThresholdReason,
			"only in client: objectives.value":                                              sloThresholdReason,
			"only in client: objectives.timeSliceTarget":                                    sloThresholdReason,
			"only in client: objectives.timeSliceWindow":                                    sloThresholdReason,
			"enum value only in client: budgetingMethod = Timeslices":                       sloThresholdReason,
			"required only in CRD: indicator":                                               sloOnlyRatioMetricReason,
			"required only in CRD: indicator.spec.ratioMetric":                              sloOnlyRatioMetricReason,
			"enum only in CRD: indicator.spec.ratioMetric.good.metricSource.type":           sloPrometheusOnlyReason,
			"enum only in CRD: indicator.spec.ratioMetric.bad.metricSource.type":            sloPrometheusOnlyReason,
			"enum only in CRD: indicator.spec.ratioMetric.total.metricSource.type":          sloPrometheusOnlyReason,
			"enum only in CRD: indicator.spec.ratioMetric.raw.metricSource.type":            sloPrometheusOnlyReason,
			"required only in CRD: indicator.spec.ratioMetric.good.metricSource.type":       sloPrometheusOnlyReason,
			"required only in CRD: indicator.spec.ratioMetric.bad.metricSource.type":        sloPrometheusOnlyReason,
			"required only in CRD: indicator.spec.ratioMetric.total.metricSource.type":      sloPrometheusOnlyReason,
			"required only in CRD: indicator.spec.ratioMetric.raw.metricSource.type":        sloPrometheusOnlyReason,
			"enum only in CRD: timeWindow.duration":                                         sloTimeWindowReason,
			"enum only in CRD: timeWindow.isRolling":                                        sloTimeWindowReason,
			"required only in CRD: timeWindow.isRolling":                                    sloTimeWindowReason,
		},
	}},
}

func notificationChannelConfigCheck(field string, clientType reflect.Type) namedCrdSchemaDriftCheck {
	return namedCrdSchemaDriftCheck{"Dash0NotificationChannel " + field, crdSchemaDriftCheck{
		crdFile:    "operator.dash0.com_dash0notificationchannels.yaml",
		version:    "v1beta1",
		schemaPath: "spec." + field,
		clientType: clientType,
	}}
}

var crdSchemaDriftChecks = func() []crdSchemaDriftCheck {
	checks := make([]crdSchemaDriftCheck, 0, len(namedCrdSchemaDriftChecks))
	for _, named := range namedCrdSchemaDriftChecks {
		checks = append(checks, named.check)
	}
	return checks
}()

func crdSchemaDriftCheckEntries() []TableEntry {
	entries := make([]TableEntry, 0, len(namedCrdSchemaDriftChecks))
	for _, named := range namedCrdSchemaDriftChecks {
		entries = append(entries, Entry(named.name, named.check))
	}
	return entries
}

// unsupportedApiClientUnionVariants lists the variants of Dash0 API client unions that the custom resources
// deliberately do not support, with the reason.
var unsupportedApiClientUnionVariants = map[reflect.Type]string{
	reflect.TypeFor[dash0apiclient.EmailConfig](): "deprecated in the Dash0 API, superseded by EmailV2Config",
}
