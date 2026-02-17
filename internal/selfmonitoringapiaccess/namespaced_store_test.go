// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//nolint:goconst
var _ = Describe(
	"SynchronizedSlice", func() {
		Context(
			"with string values", func() {
				var store *SynchronizedSlice[string]

				BeforeEach(
					func() {
						store = NewSynchronizedSlice[string]()
					},
				)

				Describe(
					"NewSynchronizedSlice", func() {
						It(
							"should create a non-nil store", func() {
								Expect(store).NotTo(BeNil())
							},
						)
					},
				)

				Describe(
					"Set and Get", func() {
						It(
							"should store and retrieve values", func() {
								store.Set([]string{"a", "b", "c"})

								values := store.Get()
								Expect(values).To(Equal([]string{"a", "b", "c"}))
							},
						)

						It(
							"should overwrite existing values", func() {
								store.Set([]string{"old"})
								store.Set([]string{"new1", "new2"})

								values := store.Get()
								Expect(values).To(Equal([]string{"new1", "new2"}))
							},
						)

						It(
							"should return a copy, not the original slice", func() {
								store.Set([]string{"a", "b"})
								values := store.Get()
								values[0] = "modified"

								original := store.Get()
								Expect(original).To(Equal([]string{"a", "b"}))
							},
						)

						It(
							"should store a copy, not the original slice", func() {
								input := []string{"a", "b"}
								store.Set(input)
								input[0] = "modified"

								values := store.Get()
								Expect(values).To(Equal([]string{"a", "b"}))
							},
						)
					},
				)

				Describe(
					"Get", func() {
						When(
							"no values have been set", func() {
								It(
									"should return nil", func() {
										values := store.Get()
										Expect(values).To(BeNil())
									},
								)
							},
						)

						When(
							"receiver is nil", func() {
								It(
									"should return nil", func() {
										var nilStore *SynchronizedSlice[string]
										values := nilStore.Get()
										Expect(values).To(BeNil())
									},
								)
							},
						)
					},
				)

				Describe(
					"Set", func() {
						When(
							"setting nil", func() {
								It(
									"should clear the stored values", func() {
										store.Set([]string{"a"})
										store.Set(nil)

										values := store.Get()
										Expect(values).To(BeNil())
									},
								)
							},
						)

						When(
							"receiver is nil", func() {
								It(
									"should not panic", func() {
										var nilStore *SynchronizedSlice[string]
										Expect(
											func() {
												nilStore.Set([]string{"a"})
											},
										).NotTo(Panic())
									},
								)
							},
						)
					},
				)

				Describe(
					"Clear", func() {
						It(
							"should remove all elements", func() {
								store.Set([]string{"a", "b", "c"})
								store.Clear()

								values := store.Get()
								Expect(values).To(BeNil())
							},
						)

						When(
							"receiver is nil", func() {
								It(
									"should not panic", func() {
										var nilStore *SynchronizedSlice[string]
										Expect(
											func() {
												nilStore.Clear()
											},
										).NotTo(Panic())
									},
								)
							},
						)
					},
				)

				Describe(
					"Concurrent access", func() {
						It(
							"should handle concurrent reads, writes, and clears safely", func() {
								iterations := 1000

								var wg sync.WaitGroup

								// Concurrent writers
								wg.Add(1)
								go func() {
									defer GinkgoRecover()
									defer wg.Done()
									for i := 0; i < iterations; i++ {
										store.Set([]string{fmt.Sprintf("val-%d", i)})
									}
								}()

								// Concurrent readers
								wg.Add(1)
								go func() {
									defer GinkgoRecover()
									defer wg.Done()
									for i := 0; i < iterations; i++ {
										store.Get()
									}
								}()

								// Concurrent clearers
								wg.Add(1)
								go func() {
									defer GinkgoRecover()
									defer wg.Done()
									for i := 0; i < iterations/10; i++ {
										store.Clear()
									}
								}()

								wg.Wait()
							},
						)
					},
				)
			},
		)

		Context(
			"with struct values", func() {
				type Endpoint struct {
					URL  string
					Port int
				}

				var store *SynchronizedSlice[Endpoint]

				BeforeEach(
					func() {
						store = NewSynchronizedSlice[Endpoint]()
					},
				)

				Describe(
					"Set and Get", func() {
						It(
							"should store and retrieve struct values", func() {
								endpoints := []Endpoint{
									{URL: "https://api.example.com", Port: 443},
									{URL: "https://backup.example.com", Port: 8443},
								}
								store.Set(endpoints)

								values := store.Get()
								Expect(values).To(Equal(endpoints))
							},
						)
					},
				)
			},
		)
	},
)

var _ = Describe(
	"SynchronizedMapSlice", func() {
		Context(
			"with string values", func() {
				var store *SynchronizedMapSlice[string]

				BeforeEach(
					func() {
						store = NewSynchronizedMapSlice[string]()
					},
				)

				Describe(
					"NewSynchronizedMapSlice", func() {
						It(
							"should create a non-nil store", func() {
								Expect(store).NotTo(BeNil())
							},
						)
					},
				)

				Describe(
					"Set and Get", func() {
						It(
							"should store and retrieve a slice for a key", func() {
								store.Set("default", []string{"token-a", "token-b"})

								values, exists := store.Get("default")
								Expect(exists).To(BeTrue())
								Expect(values).To(Equal([]string{"token-a", "token-b"}))
							},
						)

						It(
							"should store and retrieve slices for multiple keys", func() {
								store.Set("ns1", []string{"a1", "a2"})
								store.Set("ns2", []string{"b1"})
								store.Set("ns3", []string{"c1", "c2", "c3"})

								v1, exists := store.Get("ns1")
								Expect(exists).To(BeTrue())
								Expect(v1).To(Equal([]string{"a1", "a2"}))

								v2, exists := store.Get("ns2")
								Expect(exists).To(BeTrue())
								Expect(v2).To(Equal([]string{"b1"}))

								v3, exists := store.Get("ns3")
								Expect(exists).To(BeTrue())
								Expect(v3).To(Equal([]string{"c1", "c2", "c3"}))
							},
						)

						It(
							"should overwrite existing values for the same key", func() {
								store.Set("default", []string{"old"})
								store.Set("default", []string{"new1", "new2"})

								values, exists := store.Get("default")
								Expect(exists).To(BeTrue())
								Expect(values).To(Equal([]string{"new1", "new2"}))
							},
						)

						It(
							"should return a copy, not the original slice", func() {
								store.Set("default", []string{"a", "b"})
								values, _ := store.Get("default")
								values[0] = "modified"

								original, _ := store.Get("default")
								Expect(original).To(Equal([]string{"a", "b"}))
							},
						)

						It(
							"should store a copy, not the original slice", func() {
								input := []string{"a", "b"}
								store.Set("default", input)
								input[0] = "modified"

								values, _ := store.Get("default")
								Expect(values).To(Equal([]string{"a", "b"}))
							},
						)

						It(
							"should remove the entry when setting nil", func() {
								store.Set("default", []string{"a"})
								_, exists := store.Get("default")
								Expect(exists).To(BeTrue())

								store.Set("default", nil)
								values, exists := store.Get("default")
								Expect(exists).To(BeFalse())
								Expect(values).To(BeNil())
							},
						)
					},
				)

				Describe(
					"Get", func() {
						When(
							"key does not exist", func() {
								It(
									"should return false and nil", func() {
										values, exists := store.Get("non-existent")
										Expect(exists).To(BeFalse())
										Expect(values).To(BeNil())
									},
								)
							},
						)

						When(
							"receiver is nil", func() {
								It(
									"should return false and nil", func() {
										var nilStore *SynchronizedMapSlice[string]
										values, exists := nilStore.Get("key")
										Expect(exists).To(BeFalse())
										Expect(values).To(BeNil())
									},
								)
							},
						)
					},
				)

				Describe(
					"Set", func() {
						When(
							"receiver is nil", func() {
								It(
									"should not panic", func() {
										var nilStore *SynchronizedMapSlice[string]
										Expect(
											func() {
												nilStore.Set("key", []string{"a"})
											},
										).NotTo(Panic())
									},
								)
							},
						)
					},
				)

				Describe(
					"Delete", func() {
						It(
							"should remove an existing entry", func() {
								store.Set("default", []string{"a", "b"})

								_, exists := store.Get("default")
								Expect(exists).To(BeTrue(), "entry should exist before delete")

								store.Delete("default")

								values, exists := store.Get("default")
								Expect(exists).To(BeFalse())
								Expect(values).To(BeNil())
							},
						)

						It(
							"should not affect other keys", func() {
								store.Set("ns1", []string{"a"})
								store.Set("ns2", []string{"b"})

								store.Delete("ns1")

								_, exists := store.Get("ns1")
								Expect(exists).To(BeFalse())

								values, exists := store.Get("ns2")
								Expect(exists).To(BeTrue())
								Expect(values).To(Equal([]string{"b"}))
							},
						)

						It(
							"should not panic when deleting non-existent key", func() {
								Expect(
									func() {
										store.Delete("non-existent")
									},
								).NotTo(Panic())
							},
						)

						When(
							"receiver is nil", func() {
								It(
									"should not panic", func() {
										var nilStore *SynchronizedMapSlice[string]
										Expect(
											func() {
												nilStore.Delete("key")
											},
										).NotTo(Panic())
									},
								)
							},
						)
					},
				)

				Describe(
					"Edge cases", func() {
						When(
							"key is empty string", func() {
								It(
									"should store and retrieve the value", func() {
										store.Set("", []string{"val"})

										values, exists := store.Get("")
										Expect(exists).To(BeTrue())
										Expect(values).To(Equal([]string{"val"}))
									},
								)
							},
						)

						When(
							"value is an empty slice", func() {
								It(
									"should store and retrieve the empty slice", func() {
										store.Set("default", []string{})

										values, exists := store.Get("default")
										Expect(exists).To(BeTrue())
										Expect(values).To(BeEmpty())
									},
								)
							},
						)
					},
				)

				Describe(
					"Concurrent access", func() {
						It(
							"should handle concurrent reads, writes, and deletes safely", func() {
								keys := []string{"ns1", "ns2", "ns3", "ns4", "ns5"}
								iterations := 1000

								var wg sync.WaitGroup

								// Concurrent writers
								for _, k := range keys {
									wg.Add(1)
									go func(key string) {
										defer GinkgoRecover()
										defer wg.Done()
										for i := 0; i < iterations; i++ {
											store.Set(key, []string{fmt.Sprintf("val-%d", i)})
										}
									}(k)
								}

								// Concurrent readers
								for _, k := range keys {
									wg.Add(1)
									go func(key string) {
										defer GinkgoRecover()
										defer wg.Done()
										for i := 0; i < iterations; i++ {
											store.Get(key)
										}
									}(k)
								}

								// Concurrent deleters
								for _, k := range keys {
									wg.Add(1)
									go func(key string) {
										defer GinkgoRecover()
										defer wg.Done()
										for i := 0; i < iterations/10; i++ {
											store.Delete(key)
										}
									}(k)
								}

								wg.Wait()
							},
						)
					},
				)
			},
		)

		Context(
			"with struct values", func() {
				type Endpoint struct {
					URL  string
					Port int
				}

				var store *SynchronizedMapSlice[Endpoint]

				BeforeEach(
					func() {
						store = NewSynchronizedMapSlice[Endpoint]()
					},
				)

				Describe(
					"Set and Get", func() {
						It(
							"should store and retrieve struct slices", func() {
								endpoints := []Endpoint{
									{URL: "https://api.example.com", Port: 443},
									{URL: "https://backup.example.com", Port: 8443},
								}
								store.Set("default", endpoints)

								values, exists := store.Get("default")
								Expect(exists).To(BeTrue())
								Expect(values).To(Equal(endpoints))
							},
						)
					},
				)

				Describe(
					"Get", func() {
						When(
							"key does not exist", func() {
								It(
									"should return false and nil", func() {
										values, exists := store.Get("non-existent")
										Expect(exists).To(BeFalse())
										Expect(values).To(BeNil())
									},
								)
							},
						)
					},
				)

				Describe(
					"Delete", func() {
						It(
							"should remove an existing struct slice", func() {
								endpoints := []Endpoint{
									{URL: "https://api.example.com", Port: 443},
								}
								store.Set("default", endpoints)

								store.Delete("default")

								values, exists := store.Get("default")
								Expect(exists).To(BeFalse())
								Expect(values).To(BeNil())
							},
						)
					},
				)
			},
		)
	},
)

var _ = Describe(
	"NamespaceMutex", func() {
		var nm *NamespaceMutex

		BeforeEach(
			func() {
				nm = NewNamespaceMutex()
			},
		)

		Describe(
			"NewNamespaceMutex", func() {
				It(
					"should create a non-nil mutex", func() {
						Expect(nm).NotTo(BeNil())
					},
				)
			},
		)

		Describe(
			"Lock and Unlock", func() {
				It(
					"should lock and unlock without deadlock", func() {
						nm.Lock("key1")
						nm.Unlock("key1")
					},
				)

				It(
					"should allow locking different keys independently", func() {
						nm.Lock("key1")
						nm.Lock("key2")
						nm.Unlock("key1")
						nm.Unlock("key2")
					},
				)

				It(
					"should serialize access to the same key", func() {
						counter := 0
						iterations := 100

						var wg sync.WaitGroup
						for i := 0; i < iterations; i++ {
							wg.Add(1)
							go func() {
								defer GinkgoRecover()
								defer wg.Done()
								nm.Lock("shared")
								current := counter
								current++
								counter = current
								nm.Unlock("shared")
							}()
						}

						wg.Wait()
						Expect(counter).To(Equal(iterations))
					},
				)

				It(
					"should allow parallel access to different keys", func() {
						keys := []string{"key1", "key2", "key3"}
						counters := map[string]int{}
						iterations := 100

						var mu sync.Mutex
						var wg sync.WaitGroup

						for _, k := range keys {
							for i := 0; i < iterations; i++ {
								wg.Add(1)
								go func(key string) {
									defer GinkgoRecover()
									defer wg.Done()
									nm.Lock(key)
									mu.Lock()
									counters[key]++
									mu.Unlock()
									nm.Unlock(key)
								}(k)
							}
						}

						wg.Wait()
						for _, k := range keys {
							Expect(counters[k]).To(Equal(iterations))
						}
					},
				)
			},
		)

		Describe(
			"Unlock", func() {
				When(
					"key was never locked", func() {
						It(
							"should not panic", func() {
								Expect(
									func() {
										nm.Unlock("never-locked")
									},
								).NotTo(Panic())
							},
						)
					},
				)

				When(
					"receiver is nil", func() {
						It(
							"should not panic", func() {
								var nilNm *NamespaceMutex
								Expect(
									func() {
										nilNm.Unlock("key")
									},
								).NotTo(Panic())
							},
						)
					},
				)
			},
		)

		Describe(
			"Lock", func() {
				When(
					"receiver is nil", func() {
						It(
							"should not panic", func() {
								var nilNm *NamespaceMutex
								Expect(
									func() {
										nilNm.Lock("key")
									},
								).NotTo(Panic())
							},
						)
					},
				)
			},
		)

		Describe(
			"Delete", func() {
				It(
					"should remove the mutex for a key", func() {
						nm.Lock("key1")
						nm.Unlock("key1")

						nm.Delete("key1")

						// After delete, locking again should work (creates a new mutex)
						nm.Lock("key1")
						nm.Unlock("key1")
					},
				)

				It(
					"should not panic when deleting a non-existent key", func() {
						Expect(
							func() {
								nm.Delete("non-existent")
							},
						).NotTo(Panic())
					},
				)

				When(
					"receiver is nil", func() {
						It(
							"should not panic", func() {
								var nilNm *NamespaceMutex
								Expect(
									func() {
										nilNm.Delete("key")
									},
								).NotTo(Panic())
							},
						)
					},
				)
			},
		)
	},
)
