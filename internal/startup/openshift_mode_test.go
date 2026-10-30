// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"os"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("resolving OpenShift mode", func() {
	BeforeEach(func() {
		Expect(os.Unsetenv(podOpenShiftSccEnvVarName)).To(Succeed())
		Expect(os.Unsetenv("DASH0_OPENSHIFT")).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv(podOpenShiftSccEnvVarName)).To(Succeed())
		Expect(os.Unsetenv("DASH0_OPENSHIFT")).To(Succeed())
	})

	DescribeTable("resolves OpenShift mode",
		func(args []string, openShiftEnvVar string, scc string, expected bool) {
			if openShiftEnvVar != "" {
				Expect(os.Setenv("DASH0_OPENSHIFT", openShiftEnvVar)).To(Succeed())
			}
			if scc != "" {
				Expect(os.Setenv(podOpenShiftSccEnvVarName, scc)).To(Succeed())
			}
			fs, cliArgs := freshOperatorFlagSet()
			Expect(fs.Parse(args)).To(Succeed())
			_, err := applyEnvironmentVariableDefaults(fs)
			Expect(err).NotTo(HaveOccurred())

			Expect(resolveOpenShiftMode(fs, cliArgs.isOpenShift, logd.Discard())).To(Equal(expected))
		},
		Entry("disabled when nothing is set", nil, "", "", false),
		Entry("detected via the SCC annotation", nil, "", "restricted-v2", true),
		Entry("explicit argument true without the SCC annotation", []string{"--dash0-openshift=true"}, "", "", true),
		Entry("explicit argument false overrides the SCC annotation",
			[]string{"--dash0-openshift=false"}, "", "restricted-v2", false),
		Entry("environment variable true without the SCC annotation", nil, "true", "", true),
		Entry("environment variable false overrides the SCC annotation", nil, "false", "restricted-v2", false),
	)
})
