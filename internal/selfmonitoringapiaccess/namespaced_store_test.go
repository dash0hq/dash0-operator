// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NamespacedStore", func() {
	Context("with string values", func() {
		var store *NamespacedStore[string]

		BeforeEach(func() {
			store = NewNamespacedStore[string]()
		})

		Describe("NewNamespacedStore", func() {
			It("should create a non-nil store", func() {
				Expect(store).NotTo(BeNil())
			})

			It("should initialize the values map", func() {
				Expect(store.values).NotTo(BeNil())
			})
		})

		Describe("Set and Get", func() {
			It("should store and retrieve a single value", func() {
				store.Set("default", "my-token")

				value, exists := store.Get("default")
				Expect(exists).To(BeTrue())
				Expect(value).To(Equal("my-token"))
			})

			It("should store and retrieve multiple values", func() {
				testCases := map[string]string{
					"default":     "token-default",
					"kube-system": "token-kube-system",
					"my-app":      "Bearer xyz123",
				}

				for ns, tok := range testCases {
					store.Set(ns, tok)
				}

				for ns, expectedValue := range testCases {
					value, exists := store.Get(ns)
					Expect(exists).To(BeTrue(), "value should exist for namespace %s", ns)
					Expect(value).To(Equal(expectedValue))
				}
			})

			It("should overwrite existing values", func() {
				store.Set("default", "old-token")
				store.Set("default", "new-token")

				value, exists := store.Get("default")
				Expect(exists).To(BeTrue())
				Expect(value).To(Equal("new-token"))
			})
		})

		Describe("Get", func() {
			When("namespace does not exist", func() {
				It("should return false and zero value", func() {
					value, exists := store.Get("non-existent")
					Expect(exists).To(BeFalse())
					Expect(value).To(BeEmpty())
				})
			})
		})

		Describe("Delete", func() {
			It("should remove an existing value", func() {
				store.Set("default", "my-token")

				_, exists := store.Get("default")
				Expect(exists).To(BeTrue(), "value should exist before delete")

				store.Delete("default")

				value, exists := store.Get("default")
				Expect(exists).To(BeFalse())
				Expect(value).To(BeEmpty())
			})

			It("should not panic when deleting non-existent namespace", func() {
				Expect(func() {
					store.Delete("non-existent")
				}).NotTo(Panic())
			})
		})

		Describe("Edge cases", func() {
			When("namespace is empty string", func() {
				It("should store and retrieve the value", func() {
					store.Set("", "token-for-empty")

					value, exists := store.Get("")
					Expect(exists).To(BeTrue())
					Expect(value).To(Equal("token-for-empty"))
				})
			})

			When("value is empty string", func() {
				It("should store and retrieve the empty value", func() {
					store.Set("default", "")

					value, exists := store.Get("default")
					Expect(exists).To(BeTrue())
					Expect(value).To(BeEmpty())
				})
			})
		})

		Describe("Concurrent access", func() {
			It("should handle concurrent reads, writes, and deletes safely", func() {
				namespaces := []string{"ns1", "ns2", "ns3", "ns4", "ns5"}
				iterations := 1000

				var wg sync.WaitGroup

				// Concurrent writers
				for _, ns := range namespaces {
					wg.Add(1)
					go func(namespace string) {
						defer GinkgoRecover()
						defer wg.Done()
						for i := 0; i < iterations; i++ {
							store.Set(namespace, fmt.Sprintf("token-%d", i))
						}
					}(ns)
				}

				// Concurrent readers
				for _, ns := range namespaces {
					wg.Add(1)
					go func(namespace string) {
						defer GinkgoRecover()
						defer wg.Done()
						for i := 0; i < iterations; i++ {
							store.Get(namespace)
						}
					}(ns)
				}

				// Concurrent deleters
				for _, ns := range namespaces {
					wg.Add(1)
					go func(namespace string) {
						defer GinkgoRecover()
						defer wg.Done()
						for i := 0; i < iterations/10; i++ {
							store.Delete(namespace)
						}
					}(ns)
				}

				wg.Wait()
			})
		})
	})

	Context("with struct values", func() {
		type ApiConfig struct {
			Endpoint string
			Timeout  int
		}

		var store *NamespacedStore[ApiConfig]

		BeforeEach(func() {
			store = NewNamespacedStore[ApiConfig]()
		})

		Describe("Set and Get", func() {
			It("should store and retrieve struct values", func() {
				config := ApiConfig{
					Endpoint: "https://api.example.com",
					Timeout:  30,
				}
				store.Set("default", config)

				value, exists := store.Get("default")
				Expect(exists).To(BeTrue())
				Expect(value).To(Equal(config))
			})

			It("should store and retrieve multiple struct values", func() {
				configs := map[string]ApiConfig{
					"dev": {
						Endpoint: "https://dev.example.com",
						Timeout:  10,
					},
					"prod": {
						Endpoint: "https://prod.example.com",
						Timeout:  60,
					},
				}

				for ns, cfg := range configs {
					store.Set(ns, cfg)
				}

				for ns, expectedConfig := range configs {
					value, exists := store.Get(ns)
					Expect(exists).To(BeTrue())
					Expect(value).To(Equal(expectedConfig))
				}
			})
		})

		Describe("Get", func() {
			When("namespace does not exist", func() {
				It("should return false and zero value struct", func() {
					value, exists := store.Get("non-existent")
					Expect(exists).To(BeFalse())
					Expect(value).To(Equal(ApiConfig{}))
				})
			})
		})

		Describe("Delete", func() {
			It("should remove an existing struct value", func() {
				config := ApiConfig{
					Endpoint: "https://api.example.com",
					Timeout:  30,
				}
				store.Set("default", config)

				store.Delete("default")

				value, exists := store.Get("default")
				Expect(exists).To(BeFalse())
				Expect(value).To(Equal(ApiConfig{}))
			})
		})
	})

	Context("with pointer values", func() {
		type ApiConfig struct {
			Endpoint string
			Timeout  int
		}

		var store *NamespacedStore[*ApiConfig]

		BeforeEach(func() {
			store = NewNamespacedStore[*ApiConfig]()
		})

		Describe("Set and Get", func() {
			It("should store and retrieve pointer values", func() {
				config := &ApiConfig{
					Endpoint: "https://api.example.com",
					Timeout:  30,
				}
				store.Set("default", config)

				value, exists := store.Get("default")
				Expect(exists).To(BeTrue())
				Expect(value).To(Equal(config))
				Expect(value).To(BeIdenticalTo(config))
			})
		})

		Describe("Get", func() {
			When("namespace does not exist", func() {
				It("should return false and nil", func() {
					value, exists := store.Get("non-existent")
					Expect(exists).To(BeFalse())
					Expect(value).To(BeNil())
				})
			})
		})
	})
})
