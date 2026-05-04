// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0NotificationChannel is the Schema for the Dash0NotificationChannel API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0NotificationChannel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0NotificationChannelSpec   `json:"spec,omitempty"`
	Status Dash0NotificationChannelStatus `json:"status,omitempty"`
}

// Dash0NotificationChannelSpec defines the desired state of Dash0NotificationChannel.
//
// Exactly one of the type-specific config fields (slackConfig, slackBotConfig, emailV2Config, webhookConfig,
// incidentioConfig, opsgenieConfig, pagerdutyConfig, teamsWebhookConfig, discordWebhookConfig,
// googleChatWebhookConfig, ilertConfig, allQuietConfig) must be set, matching the value of the type field.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'slack' ? has(self.slackConfig) : !has(self.slackConfig)",message="slackConfig must be set when type is 'slack', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'slack_bot' ? has(self.slackBotConfig) : !has(self.slackBotConfig)",message="slackBotConfig must be set when type is 'slack_bot', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'email_v2' ? has(self.emailV2Config) : !has(self.emailV2Config)",message="emailV2Config must be set when type is 'email_v2', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'webhook' ? has(self.webhookConfig) : !has(self.webhookConfig)",message="webhookConfig must be set when type is 'webhook', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'incidentio' ? has(self.incidentioConfig) : !has(self.incidentioConfig)",message="incidentioConfig must be set when type is 'incidentio', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'opsgenie' ? has(self.opsgenieConfig) : !has(self.opsgenieConfig)",message="opsgenieConfig must be set when type is 'opsgenie', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'pagerduty' ? has(self.pagerdutyConfig) : !has(self.pagerdutyConfig)",message="pagerdutyConfig must be set when type is 'pagerduty', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'teams_webhook' ? has(self.teamsWebhookConfig) : !has(self.teamsWebhookConfig)",message="teamsWebhookConfig must be set when type is 'teams_webhook', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'discord_webhook' ? has(self.discordWebhookConfig) : !has(self.discordWebhookConfig)",message="discordWebhookConfig must be set when type is 'discord_webhook', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'google_chat_webhook' ? has(self.googleChatWebhookConfig) : !has(self.googleChatWebhookConfig)",message="googleChatWebhookConfig must be set when type is 'google_chat_webhook', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'ilert' ? has(self.ilertConfig) : !has(self.ilertConfig)",message="ilertConfig must be set when type is 'ilert', and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'all_quiet' ? has(self.allQuietConfig) : !has(self.allQuietConfig)",message="allQuietConfig must be set when type is 'all_quiet', and must not be set otherwise"
type Dash0NotificationChannelSpec struct {
	// Display configuration for the notification channel.
	// +kubebuilder:validation:Required
	Display Dash0NotificationChannelDisplay `json:"display"`

	// The type of notification channel integration.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=slack;slack_bot;email;email_v2;webhook;incidentio;prometheus_webhook;betterstack;prometheus_alertmanager;opsgenie;pagerduty;jira_service_management_ops;teams_webhook;discord_webhook;google_chat_webhook;ilert;all_quiet
	Type string `json:"type"`

	// Configuration for Slack webhook notifications. Required when type is "slack".
	// +kubebuilder:validation:Optional
	SlackConfig *SlackConfig `json:"slackConfig,omitempty"`

	// Configuration for Slack Bot notifications. Required when type is "slack_bot".
	// +kubebuilder:validation:Optional
	SlackBotConfig *SlackBotConfig `json:"slackBotConfig,omitempty"`

	// Configuration for email (v2) notifications. Required when type is "email_v2".
	// +kubebuilder:validation:Optional
	EmailV2Config *EmailV2Config `json:"emailV2Config,omitempty"`

	// Configuration for generic webhook notifications. Required when type is "webhook".
	// +kubebuilder:validation:Optional
	WebhookConfig *WebhookConfig `json:"webhookConfig,omitempty"`

	// Configuration for Incident.io notifications. Required when type is "incidentio".
	// +kubebuilder:validation:Optional
	IncidentioConfig *IncidentioConfig `json:"incidentioConfig,omitempty"`

	// Configuration for OpsGenie notifications. Required when type is "opsgenie".
	// +kubebuilder:validation:Optional
	OpsgenieConfig *OpsgenieConfig `json:"opsgenieConfig,omitempty"`

	// Configuration for PagerDuty notifications. Required when type is "pagerduty".
	// +kubebuilder:validation:Optional
	PagerdutyConfig *PagerdutyConfig `json:"pagerdutyConfig,omitempty"`

	// Configuration for Microsoft Teams webhook notifications. Required when type is "teams_webhook".
	// +kubebuilder:validation:Optional
	TeamsWebhookConfig *TeamsWebhookConfig `json:"teamsWebhookConfig,omitempty"`

	// Configuration for Discord webhook notifications. Required when type is "discord_webhook".
	// +kubebuilder:validation:Optional
	DiscordWebhookConfig *DiscordWebhookConfig `json:"discordWebhookConfig,omitempty"`

	// Configuration for Google Chat webhook notifications. Required when type is "google_chat_webhook".
	// +kubebuilder:validation:Optional
	GoogleChatWebhookConfig *GoogleChatWebhookConfig `json:"googleChatWebhookConfig,omitempty"`

	// Configuration for iLert notifications. Required when type is "ilert".
	// +kubebuilder:validation:Optional
	IlertConfig *IlertConfig `json:"ilertConfig,omitempty"`

	// Configuration for All Quiet notifications. Required when type is "all_quiet".
	// +kubebuilder:validation:Optional
	AllQuietConfig *AllQuietConfig `json:"allQuietConfig,omitempty"`

	// Specifies the notification frequency. Optional, defaults to "10m".
	// +kubebuilder:validation:Optional
	Frequency string `json:"frequency,omitempty"`

	// Routing configuration for the notification channel. When set, routing updates will be executed. When omitted,
	// no routing changes will be made.
	// +kubebuilder:validation:Optional
	Routing *Dash0NotificationChannelRouting `json:"routing,omitempty"`
}

// Dash0NotificationChannelDisplay defines the display configuration for a notification channel.
type Dash0NotificationChannelDisplay struct {
	// The display name of the notification channel.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// SlackConfig defines the configuration for Slack webhook notifications.
type SlackConfig struct {
	// The Slack incoming webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	WebhookURL string `json:"webhookURL"`

	// The Slack channel to post to.
	// +kubebuilder:validation:Required
	Channel string `json:"channel"`
}

// SlackBotConfig defines the configuration for Slack Bot notifications.
type SlackBotConfig struct {
	// The Slack team (workspace) ID.
	// +kubebuilder:validation:Required
	TeamID string `json:"teamId"`

	// The Slack channel to post to.
	// +kubebuilder:validation:Required
	Channel string `json:"channel"`
}

// EmailV2Config defines the configuration for email (v2) notifications.
type EmailV2Config struct {
	// The list of email recipients.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Recipients []string `json:"recipients"`

	// Whether to send emails in plaintext format.
	// +kubebuilder:validation:Optional
	Plaintext *bool `json:"plaintext,omitempty"`
}

// WebhookConfig defines the configuration for generic webhook notifications.
type WebhookConfig struct {
	// The webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`

	// Additional HTTP headers to include in the webhook request.
	// +kubebuilder:validation:Optional
	Headers map[string]string `json:"headers,omitempty"`

	// Whether to follow HTTP redirects.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	FollowRedirects *bool `json:"followRedirects,omitempty"`

	// Whether to allow insecure (non-TLS) connections.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	AllowInsecure *bool `json:"allowInsecure,omitempty"`
}

// IncidentioConfig defines the configuration for Incident.io notifications.
type IncidentioConfig struct {
	// The Incident.io webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`

	// The authorization header value for Incident.io.
	// +kubebuilder:validation:Required
	Headers string `json:"headers"`
}

// OpsgenieConfig defines the configuration for OpsGenie notifications.
type OpsgenieConfig struct {
	// The OpsGenie instance region.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=eu;us
	Instance string `json:"instance"`

	// The OpsGenie API key.
	// +kubebuilder:validation:Required
	ApiKey string `json:"apiKey"`
}

// PagerdutyConfig defines the configuration for PagerDuty notifications.
type PagerdutyConfig struct {
	// The PagerDuty integration key.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// The PagerDuty events API URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// TeamsWebhookConfig defines the configuration for Microsoft Teams webhook notifications.
type TeamsWebhookConfig struct {
	// The Microsoft Teams incoming webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// DiscordWebhookConfig defines the configuration for Discord webhook notifications.
type DiscordWebhookConfig struct {
	// The Discord webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// GoogleChatWebhookConfig defines the configuration for Google Chat webhook notifications.
type GoogleChatWebhookConfig struct {
	// The Google Chat webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// IlertConfig defines the configuration for iLert notifications.
type IlertConfig struct {
	// The iLert alert source URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// AllQuietConfig defines the configuration for All Quiet notifications.
type AllQuietConfig struct {
	// The All Quiet webhook URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	URL string `json:"url"`
}

// Dash0NotificationChannelRouting defines the routing configuration for a notification channel.
type Dash0NotificationChannelRouting struct {
	// The assets to route notifications for.
	// +kubebuilder:validation:Required
	Assets []Dash0NotificationChannelRoutingAsset `json:"assets"`

	// Filter criteria for routing notifications.
	// +kubebuilder:validation:Required
	Filters []Dash0NotificationChannelRoutingFilter `json:"filters"`
}

// Dash0NotificationChannelRoutingAsset defines a routing asset.
type Dash0NotificationChannelRoutingAsset struct {
	// The kind of asset.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=check_rule;synthetic_check
	Kind string `json:"kind"`

	// The ID of the asset.
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// The name of the asset.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// The dataset of the asset.
	// +kubebuilder:validation:Required
	Dataset string `json:"dataset"`
}

// Dash0NotificationChannelRoutingFilter defines a filter condition for notification routing.
type Dash0NotificationChannelRoutingFilter struct {
	// The attribute key to be filtered.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// The match operation for filtering attributes.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
	Operator string `json:"operator"`

	// The value to match against.
	// +kubebuilder:validation:Optional
	Value string `json:"value,omitempty"`

	// List of values to match against. This parameter is mandatory for the is_one_of and is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0NotificationChannelStatus defines the observed state of Dash0NotificationChannel
type Dash0NotificationChannelStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus                    `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                          `json:"synchronizedAt"`
	ValidationIssues       []string                                                             `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0NotificationChannelSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0NotificationChannelSynchronizationResultPerEndpointAndDataset defines the synchronization result per endpoint
type Dash0NotificationChannelSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
}

// Dash0NotificationChannelList contains a list of Dash0NotificationChannel resources.
//
// +kubebuilder:object:root=true
type Dash0NotificationChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0NotificationChannel `json:"items"`
}
