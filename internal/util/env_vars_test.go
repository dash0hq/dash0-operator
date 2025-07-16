// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("env var utils", func() {

	It("GetEnvVar", func() {
		Expect(GetEnvVar(nil, "VAR_NAME")).To(BeNil())
		Expect(GetEnvVar(&corev1.Container{}, "VAR_NAME")).To(BeNil())
		Expect(
			GetEnvVar(&corev1.Container{Env: []corev1.EnvVar{{Name: "VAR_NAME", Value: "value"}}}, "VAR_NAME"),
		).To(Equal(&corev1.EnvVar{Name: "VAR_NAME", Value: "value"}))
		Expect(
			GetEnvVar(&corev1.Container{Env: []corev1.EnvVar{{Name: "VAR_NAME", Value: "value"}}}, "ANOTHER_VAR"),
		).To(BeNil())

	})

	It("IsEnvVarUnsetOrEmpty", func() {
		Expect(IsEnvVarUnsetOrEmpty(nil)).To(BeTrue())
		Expect(IsEnvVarUnsetOrEmpty(&corev1.EnvVar{Value: ""})).To(BeTrue())
		Expect(IsEnvVarUnsetOrEmpty(&corev1.EnvVar{Value: "  "})).To(BeTrue())
		Expect(IsEnvVarUnsetOrEmpty(&corev1.EnvVar{Value: " \t \n "})).To(BeTrue())
		Expect(IsEnvVarUnsetOrEmpty(&corev1.EnvVar{Value: "..."})).To(BeFalse())
		Expect(IsEnvVarUnsetOrEmpty(&corev1.EnvVar{ValueFrom: &corev1.EnvVarSource{}})).To(BeFalse())
	})
})
