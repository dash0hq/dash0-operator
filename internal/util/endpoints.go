// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import "strings"

// DeriveDecisionMakerEndpoint transforms a Dash0 ingress endpoint into the corresponding Decision Maker endpoint.
func DeriveDecisionMakerEndpoint(ingressEndpoint string) string {
	endpoint := strings.Replace(ingressEndpoint, "ingress.", "decision-maker.", 1)
	endpoint = strings.Replace(endpoint, ":4317", ":443", 1)
	return endpoint
}
