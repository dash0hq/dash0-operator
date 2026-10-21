// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebhookCertificates(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Certificates Suite")
}
