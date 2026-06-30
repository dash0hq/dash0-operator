// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	corev1 "k8s.io/api/core/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("env var utils", func() {

	Describe("CreateEnvVarForSecretKeySelector", func() {
		It("creates an env var sourcing its value from the secret", func() {
			envVar, err := CreateEnvVarForSecretKeySelector(
				&dash0common.SecretKeySelector{Name: "my-secret", Key: "my-key"},
				"MY_ENV_VAR",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(envVar).To(Equal(corev1.EnvVar{
				Name: "MY_ENV_VAR",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-secret",
						},
						Key: "my-key",
					},
				},
			}))
		})

		It("returns an error when the secretKeyRef is nil", func() {
			envVar, err := CreateEnvVarForSecretKeySelector(nil, "MY_ENV_VAR")
			Expect(err).To(MatchError(
				"no valid secretKeyRef (name and key) provided for env var MY_ENV_VAR"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})

		It("returns an error when the secret name is missing", func() {
			envVar, err := CreateEnvVarForSecretKeySelector(
				&dash0common.SecretKeySelector{Key: "my-key"},
				"MY_ENV_VAR",
			)
			Expect(err).To(MatchError(
				"no valid secretKeyRef (name and key) provided for env var MY_ENV_VAR"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})

		It("returns an error when the secret key is missing", func() {
			envVar, err := CreateEnvVarForSecretKeySelector(
				&dash0common.SecretKeySelector{Name: "my-secret"},
				"MY_ENV_VAR",
			)
			Expect(err).To(MatchError(
				"no valid secretKeyRef (name and key) provided for env var MY_ENV_VAR"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})
	})

	Describe("CreateEnvVarForAuthorization", func() {
		token := "my-token"
		emptyToken := ""

		It("creates an env var with the token value", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{Token: &token},
				"AUTH_TOKEN",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(envVar).To(Equal(corev1.EnvVar{
				Name:  "AUTH_TOKEN",
				Value: "my-token",
			}))
		})

		It("prefers the token over the secretRef when both are provided", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{
					Token:     &token,
					SecretRef: &dash0common.SecretRef{Name: "my-secret", Key: "my-key"},
				},
				"AUTH_TOKEN",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(envVar).To(Equal(corev1.EnvVar{
				Name:  "AUTH_TOKEN",
				Value: "my-token",
			}))
		})

		It("creates an env var sourcing its value from the secret when only the secretRef is provided", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{
					SecretRef: &dash0common.SecretRef{Name: "my-secret", Key: "my-key"},
				},
				"AUTH_TOKEN",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(envVar).To(Equal(corev1.EnvVar{
				Name: "AUTH_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-secret",
						},
						Key: "my-key",
					},
				},
			}))
		})

		It("falls back to the secretRef when the token is set but empty", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{
					Token:     &emptyToken,
					SecretRef: &dash0common.SecretRef{Name: "my-secret", Key: "my-key"},
				},
				"AUTH_TOKEN",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(envVar).To(Equal(corev1.EnvVar{
				Name: "AUTH_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-secret",
						},
						Key: "my-key",
					},
				},
			}))
		})

		It("returns an error when neither token nor secretRef is provided", func() {
			envVar, err := CreateEnvVarForAuthorization(dash0common.Authorization{}, "AUTH_TOKEN")
			Expect(err).To(MatchError("neither token nor secretRef provided for the Dash0 exporter"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})

		It("returns an error when the secretRef is missing the name", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{
					SecretRef: &dash0common.SecretRef{Key: "my-key"},
				},
				"AUTH_TOKEN",
			)
			Expect(err).To(MatchError("neither token nor secretRef provided for the Dash0 exporter"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})

		It("returns an error when the secretRef is missing the key", func() {
			envVar, err := CreateEnvVarForAuthorization(
				dash0common.Authorization{
					SecretRef: &dash0common.SecretRef{Name: "my-secret"},
				},
				"AUTH_TOKEN",
			)
			Expect(err).To(MatchError("neither token nor secretRef provided for the Dash0 exporter"))
			Expect(envVar).To(Equal(corev1.EnvVar{}))
		})
	})

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
