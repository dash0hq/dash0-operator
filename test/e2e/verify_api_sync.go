// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	. "github.com/onsi/gomega"
)

func verifyApiSyncRequest(req StoredRequest) {
	Expect(*req.Body).ToNot(ContainSubstring("managedFields"))
	Expect(*req.Body).ToNot(ContainSubstring("kubectl.kubernetes.io/last-applied-configuration"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/dataset"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/id"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/source"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/version"))
}

// The sampling rule controller intentionally emits dash0.com/dataset in the API
// payload metadata.labels (see transformToSamplingApiPayload), so unlike
// verifyApiSyncRequest, that label is allowed in the body here.
func verifySamplingRuleApiSyncRequest(req StoredRequest) {
	Expect(*req.Body).ToNot(ContainSubstring("managedFields"))
	Expect(*req.Body).ToNot(ContainSubstring("kubectl.kubernetes.io/last-applied-configuration"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/id"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/source"))
	Expect(*req.Body).ToNot(ContainSubstring("dash0.com/version"))
}
