// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// allowedDash0ApiEndpointHostPatterns is the hard-coded allowlist of host patterns accepted for the Dash0 API endpoint.
// The patterns guard the controllers and webhooks against being pointed at arbitrary hosts (SSRF).
var allowedDash0ApiEndpointHostPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^api\.([a-z0-9-]+\.)*dash0\.com$`),
	regexp.MustCompile(`^api\.([a-z0-9-]+\.)*dash0-dev\.com$`),
	regexp.MustCompile(`^([a-z0-9-]+\.)+svc\.cluster\.local$`),
}

// ValidateDash0ApiEndpoint verifies that the given endpoint is one of the allowed Dash0 API endpoints. It is the central
// allowlist used both by the validating admission webhooks and by the controllers when picking up an ApiEndpoint from a
// Dash0OperatorConfiguration or Dash0Monitoring resource. Returns nil if the endpoint is acceptable, or an error
// describing why the endpoint was rejected.
func ValidateDash0ApiEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("the Dash0 API endpoint is empty")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("the Dash0 API endpoint %q is not a valid URL: %w", endpoint, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("the Dash0 API endpoint %q must use the http or https scheme", endpoint)
	}
	if parsed.User != nil {
		return fmt.Errorf("the Dash0 API endpoint %q must not contain user info", endpoint)
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("the Dash0 API endpoint %q must not contain a query string", endpoint)
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return fmt.Errorf("the Dash0 API endpoint %q must not contain a fragment", endpoint)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("the Dash0 API endpoint %q does not contain a host", endpoint)
	}
	for _, pattern := range allowedDash0ApiEndpointHostPatterns {
		if pattern.MatchString(host) {
			return nil
		}
	}
	return fmt.Errorf(
		"the Dash0 API endpoint %q has a host (%q) that is not in the allowlist of permitted Dash0 API hosts",
		endpoint,
		host,
	)
}
