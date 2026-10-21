// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The webhook certificates store", func() {
	It("reports errors until the material has been loaded and the latch has been set", func() {
		store := NewStore()
		_, err := store.GetCertificate(nil)
		Expect(err).To(HaveOccurred())
		Expect(store.ReadyCheck(nil)).ToNot(Succeed())
		Expect(store.CaBundle()).To(BeNil())

		Expect(store.load(generateTestMaterial(retentionCandidates{}))).To(Succeed())
		_, err = store.GetCertificate(nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(store.ReadyCheck(nil)).ToNot(Succeed())

		store.setReady()
		Expect(store.ReadyCheck(nil)).To(Succeed())
	})

	It("notifies the listeners when the bundle changes, or when asked to", func() {
		store := NewStore()
		notifications := make(chan []byte, 10)
		store.OnCaBundleChange(func(bundle []byte) {
			notifications <- bundle
		})
		m := generateTestMaterial(retentionCandidates{})

		Expect(store.load(m)).To(Succeed())
		Eventually(notifications).Should(Receive(Equal(m.bundle())))

		Expect(store.load(m)).To(Succeed())
		Consistently(notifications, "100ms").ShouldNot(Receive())

		store.notifyListeners()
		Eventually(notifications).Should(Receive(Equal(m.bundle())))
	})
})
