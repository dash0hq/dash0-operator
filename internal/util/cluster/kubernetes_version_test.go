// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Kubernetes Version", func() {

	Describe("parseKubernetesApiServerVersion", func() {
		type parseVersionTest struct {
			major                  string
			minor                  string
			expectedMajor          int
			expectedMinor          int
			expectedString         string
			expectedOriginalString string
			expectedDetected       bool
		}

		DescribeTable("should parse the major/minor of a Kubernetes API server version",
			func(testConfig parseVersionTest) {
				info := parseKubernetesApiServerVersion(
					&version.Info{Major: testConfig.major, Minor: testConfig.minor},
					logd.Discard(),
				)
				Expect(info.Detected).To(Equal(testConfig.expectedDetected))
				Expect(info.String()).To(Equal(testConfig.expectedString))
				Expect(info.OriginalVersionString).To(Equal(testConfig.expectedOriginalString))
				if testConfig.expectedDetected {
					Expect(info.Version.Major).To(Equal(testConfig.expectedMajor))
					Expect(info.Version.Minor).To(Equal(testConfig.expectedMinor))
				} else {
					Expect(info.Version.Major).To(Equal(0))
					Expect(info.Version.Minor).To(Equal(0))
				}
			},
			Entry("1.36", parseVersionTest{
				major:                  "1",
				minor:                  "36",
				expectedMajor:          1,
				expectedMinor:          36,
				expectedString:         "1.36",
				expectedOriginalString: "1.36",
				expectedDetected:       true,
			}),
			Entry("1.31", parseVersionTest{
				major:                  "1",
				minor:                  "31",
				expectedMajor:          1,
				expectedMinor:          31,
				expectedString:         "1.31",
				expectedOriginalString: "1.31",
				expectedDetected:       true,
			}),
			Entry("GKE-style minor with plus suffix", parseVersionTest{
				major:                  "1",
				minor:                  "31+",
				expectedMajor:          1,
				expectedMinor:          31,
				expectedString:         "1.31",
				expectedOriginalString: "1.31+",
				expectedDetected:       true,
			}),
			Entry("EKS-style minor with text suffix", parseVersionTest{
				major:                  "1",
				minor:                  "30-eks-abc1234",
				expectedMajor:          1,
				expectedMinor:          30,
				expectedString:         "1.30",
				expectedOriginalString: "1.30-eks-abc1234",
				expectedDetected:       true,
			}),
			Entry("hypothetical major version 2", parseVersionTest{
				major:                  "2",
				minor:                  "0",
				expectedMajor:          2,
				expectedMinor:          0,
				expectedString:         "2.0",
				expectedOriginalString: "2.0",
				expectedDetected:       true,
			}),
			Entry("2.34", parseVersionTest{
				major:                  "2",
				minor:                  "34",
				expectedMajor:          2,
				expectedMinor:          34,
				expectedString:         "2.34",
				expectedOriginalString: "2.34",
				expectedDetected:       true,
			}),
			Entry("empty major", parseVersionTest{
				major:                  "",
				minor:                  "31",
				expectedString:         "? (.31)",
				expectedOriginalString: ".31",
				expectedDetected:       false,
			}),
			Entry("empty minor", parseVersionTest{
				major:                  "1",
				minor:                  "",
				expectedString:         "? (1.)",
				expectedOriginalString: "1.",
				expectedDetected:       false,
			}),
			Entry("both empty", parseVersionTest{
				major:                  "",
				minor:                  "",
				expectedString:         "? (.)",
				expectedOriginalString: ".",
				expectedDetected:       false,
			}),
			Entry("non-numeric major", parseVersionTest{
				major:                  "abc",
				minor:                  "31",
				expectedString:         "? (abc.31)",
				expectedOriginalString: "abc.31",
				expectedDetected:       false,
			}),
			Entry("non-numeric minor", parseVersionTest{
				major:                  "1",
				minor:                  "thirty-one",
				expectedString:         "? (1.thirty-one)",
				expectedOriginalString: "1.thirty-one",
				expectedDetected:       false,
			}),
			Entry("minor with leading non-digit", parseVersionTest{
				major:                  "1",
				minor:                  "+31",
				expectedString:         "? (1.+31)",
				expectedOriginalString: "1.+31",
				expectedDetected:       false,
			}),
		)
	})

	Describe("extractLeadingDigits", func() {
		type extractLeadingDigitsTest struct {
			input       string
			expected    int
			expectError bool
		}

		DescribeTable("should extract the leading digits of a string",
			func(testConfig extractLeadingDigitsTest) {
				result, err := extractLeadingDigits(testConfig.input)
				if testConfig.expectError {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(testConfig.expected))
			},
			Entry("single digit", extractLeadingDigitsTest{
				input:    "1",
				expected: 1,
			}),
			Entry("multiple digits", extractLeadingDigitsTest{
				input:    "36",
				expected: 36,
			}),
			Entry("zero", extractLeadingDigitsTest{
				input:    "0",
				expected: 0,
			}),
			Entry("GKE-style suffix (digits followed by '+')", extractLeadingDigitsTest{
				input:    "31+",
				expected: 31,
			}),
			Entry("minor version with patch suffix", extractLeadingDigitsTest{
				input:    "31.0",
				expected: 31,
			}),
			Entry("EKS-style suffix (digits followed by '-text')", extractLeadingDigitsTest{
				input:    "31-eks-abc1234",
				expected: 31,
			}),
			Entry("digits followed by trailing whitespace", extractLeadingDigitsTest{
				input:    "31 ",
				expected: 31,
			}),
			Entry("empty string", extractLeadingDigitsTest{
				input:       "",
				expectError: true,
			}),
			Entry("no leading digits, only letters", extractLeadingDigitsTest{
				input:       "abc",
				expectError: true,
			}),
			Entry("leading non-digit (plus)", extractLeadingDigitsTest{
				input:       "+31",
				expectError: true,
			}),
			Entry("leading whitespace", extractLeadingDigitsTest{
				input:       " 31",
				expectError: true,
			}),
			Entry("leading dot", extractLeadingDigitsTest{
				input:       ".31",
				expectError: true,
			}),
		)
	})

	Describe("KubernetesVersionInfo.IsAtLeast", func() {
		type isAtLeastTest struct {
			actualVersion  KubernetesVersion
			minimumVersion KubernetesVersion
			expected       bool
		}

		DescribeTable("should compare the major/minor version",
			func(testConfig isAtLeastTest) {
				actualVersionInfo := KubernetesVersionInfo{Version: testConfig.actualVersion}
				Expect(
					actualVersionInfo.IsAtLeast(testConfig.minimumVersion)).
					To(Equal(testConfig.expected))
			},
			Entry("same version", isAtLeastTest{
				actualVersion:  KubernetesVersion{Major: 1, Minor: 36},
				minimumVersion: KubernetesVersion{Major: 1, Minor: 36},
				expected:       true,
			}),
			Entry("higher minor version", isAtLeastTest{
				actualVersion:  KubernetesVersion{Major: 1, Minor: 37},
				minimumVersion: KubernetesVersion{Major: 1, Minor: 36},
				expected:       true,
			}),
			Entry("lower minor version", isAtLeastTest{
				actualVersion:  KubernetesVersion{Major: 1, Minor: 35},
				minimumVersion: KubernetesVersion{Major: 1, Minor: 36},
				expected:       false,
			}),
			Entry("higher major version with lower minor version", isAtLeastTest{
				actualVersion:  KubernetesVersion{Major: 2, Minor: 0},
				minimumVersion: KubernetesVersion{Major: 1, Minor: 36},
				expected:       true,
			}),
			Entry("lower major version with higher minor version", isAtLeastTest{
				actualVersion:  KubernetesVersion{Major: 1, Minor: 99},
				minimumVersion: KubernetesVersion{Major: 2, Minor: 0},
				expected:       false,
			}),
			Entry("zero value", isAtLeastTest{
				actualVersion:  KubernetesVersion{},
				minimumVersion: KubernetesVersion{Major: 1, Minor: 31},
				expected:       false,
			}),
		)
	})

	Describe("parseKubeletVersion", func() {
		type parseKubeletVersionTest struct {
			kubeletVersion         string
			expectedMajor          int
			expectedMinor          int
			expectedString         string
			expectedOriginalString string
			expectError            bool
		}

		DescribeTable("should parse the kubelet version of a node",
			func(testConfig parseKubeletVersionTest) {
				versionInfo, err := parseKubeletVersion(testConfig.kubeletVersion, logd.Discard())
				if testConfig.expectError {
					Expect(err).To(HaveOccurred())
					Expect(versionInfo.Detected).To(BeFalse())
					Expect(versionInfo.Version.Major).To(Equal(0))
					Expect(versionInfo.Version.Minor).To(Equal(0))
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(versionInfo.Detected).To(BeTrue())
					Expect(versionInfo.Version.Major).To(Equal(testConfig.expectedMajor))
					Expect(versionInfo.Version.Minor).To(Equal(testConfig.expectedMinor))
				}
				Expect(versionInfo.String()).To(Equal(testConfig.expectedString))
				Expect(versionInfo.OriginalVersionString).To(Equal(testConfig.expectedOriginalString))
			},
			Entry("plain version", parseKubeletVersionTest{
				kubeletVersion:         "v1.36.0",
				expectedMajor:          1,
				expectedMinor:          36,
				expectedString:         "1.36",
				expectedOriginalString: "v1.36.0",
			}),
			Entry("version without the v prefix", parseKubeletVersionTest{
				kubeletVersion:         "1.31.5",
				expectedMajor:          1,
				expectedMinor:          31,
				expectedString:         "1.31",
				expectedOriginalString: "1.31.5",
			}),
			Entry("GKE-style vendor suffix", parseKubeletVersionTest{
				kubeletVersion:         "v1.31.5-gke.1000",
				expectedMajor:          1,
				expectedMinor:          31,
				expectedString:         "1.31",
				expectedOriginalString: "v1.31.5-gke.1000",
			}),
			Entry("EKS-style vendor suffix", parseKubeletVersionTest{
				kubeletVersion:         "v1.30.14-eks-3abbec1",
				expectedMajor:          1,
				expectedMinor:          30,
				expectedString:         "1.30",
				expectedOriginalString: "v1.30.14-eks-3abbec1",
			}),
			Entry("k3s-style vendor suffix", parseKubeletVersionTest{
				kubeletVersion:         "v1.33.0+k3s1",
				expectedMajor:          1,
				expectedMinor:          33,
				expectedString:         "1.33",
				expectedOriginalString: "v1.33.0+k3s1",
			}),
			Entry("minor version with suffix but no patch version", parseKubeletVersionTest{
				kubeletVersion:         "v1.36+",
				expectedMajor:          1,
				expectedMinor:          36,
				expectedString:         "1.36",
				expectedOriginalString: "v1.36+",
			}),
			Entry("surrounding whitespace", parseKubeletVersionTest{
				kubeletVersion:         " v2.0.1 ",
				expectedMajor:          2,
				expectedMinor:          0,
				expectedString:         "2.0",
				expectedOriginalString: " v2.0.1 ",
			}),
			Entry("empty string", parseKubeletVersionTest{
				kubeletVersion:         "",
				expectError:            true,
				expectedString:         "?",
				expectedOriginalString: "",
			}),
			Entry("no minor version", parseKubeletVersionTest{
				kubeletVersion:         "v1",
				expectError:            true,
				expectedString:         "? (v1)",
				expectedOriginalString: "v1",
			}),
			Entry("non-numeric minor version", parseKubeletVersionTest{
				kubeletVersion:         "v1.abc.0",
				expectError:            true,
				expectedString:         "? (v1.abc.0)",
				expectedOriginalString: "v1.abc.0",
			}),
			Entry("non-numeric major version", parseKubeletVersionTest{
				kubeletVersion:         "vX.36.0",
				expectError:            true,
				expectedString:         "? (vX.36.0)",
				expectedOriginalString: "vX.36.0",
			}),
		)
	})

	Describe("detectMinimumKubeletVersion", func() {
		type detectMinimumKubeletVersionTest struct {
			kubeletVersions        []string
			expectedDetected       bool
			expectedMajor          int
			expectedMinor          int
			expectedOriginalString string
			expectedRetryable      bool
		}

		DescribeTable("should determine the minimum kubelet version of all nodes",
			func(testConfig detectMinimumKubeletVersionTest) {
				clientset := fake.NewClientset(nodesWithKubeletVersions(testConfig.kubeletVersions)...)

				minimumKubeletVersion, err := detectMinimumKubeletVersion(context.Background(), clientset, logd.Discard())

				if !testConfig.expectedDetected {
					Expect(err).To(HaveOccurred())
					Expect(retry.IsRetryable(err)).To(Equal(testConfig.expectedRetryable))
					Expect(minimumKubeletVersion.Detected).To(BeFalse())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(minimumKubeletVersion.Detected).To(BeTrue())
				Expect(minimumKubeletVersion.Version.Major).To(Equal(testConfig.expectedMajor))
				Expect(minimumKubeletVersion.Version.Minor).To(Equal(testConfig.expectedMinor))
				Expect(minimumKubeletVersion.OriginalVersionString).To(Equal(testConfig.expectedOriginalString))
			},
			Entry("single node", detectMinimumKubeletVersionTest{
				kubeletVersions:        []string{"v1.36.0"},
				expectedDetected:       true,
				expectedMajor:          1,
				expectedMinor:          36,
				expectedOriginalString: "v1.36.0",
			}),
			Entry("all nodes on the same version", detectMinimumKubeletVersionTest{
				kubeletVersions:  []string{"v1.36.1", "v1.36.0", "v1.36.2"},
				expectedDetected: true,
				expectedMajor:    1,
				expectedMinor:    36,
				// The minimum version comparison does not differentiate between patch versions, only major and minor versions
				// are compared. If different patch versions have the same major/minor, the first one wins.
				expectedOriginalString: "v1.36.1",
			}),
			Entry("one node lagging behind", detectMinimumKubeletVersionTest{
				kubeletVersions:        []string{"v1.36.0", "v1.35.4", "v1.37.0"},
				expectedDetected:       true,
				expectedMajor:          1,
				expectedMinor:          35,
				expectedOriginalString: "v1.35.4",
			}),
			Entry("nodes with vendor suffixes", detectMinimumKubeletVersionTest{
				kubeletVersions:        []string{"v1.31.5-gke.1000", "v1.30.14-eks-3abbec1"},
				expectedDetected:       true,
				expectedMajor:          1,
				expectedMinor:          30,
				expectedOriginalString: "v1.30.14-eks-3abbec1",
			}),
			Entry("nodes across major versions", detectMinimumKubeletVersionTest{
				kubeletVersions:        []string{"v2.0.0", "v1.99.0"},
				expectedDetected:       true,
				expectedMajor:          1,
				expectedMinor:          99,
				expectedOriginalString: "v1.99.0",
			}),
			Entry("an unparseable kubelet version is a non-retryable failure", detectMinimumKubeletVersionTest{
				kubeletVersions:  []string{"v1.36.0", "not-a-version"},
				expectedDetected: false,
			}),
			Entry("a cluster without nodes is a retryable failure", detectMinimumKubeletVersionTest{
				kubeletVersions:   []string{},
				expectedDetected:  false,
				expectedRetryable: true,
			}),
		)

		It("should use pagination when inspecting nodes", func() {
			clientset := fake.NewClientset()
			listOptionsPerRequest := make([]metav1.ListOptions, 0, 2)
			clientset.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
				listOptionsPerRequest = append(listOptionsPerRequest, action.(k8stesting.ListActionImpl).ListOptions)
				if len(listOptionsPerRequest) == 1 {
					return true, &corev1.NodeList{
						ListMeta: metav1.ListMeta{Continue: "second-chunk"},
						Items: []corev1.Node{
							*nodeWithKubeletVersion("node-0", "v1.39.0"),
							*nodeWithKubeletVersion("node-0", "v1.38.0"),
							*nodeWithKubeletVersion("node-0", "v1.37.0"),
						},
					}, nil
				}
				return true, &corev1.NodeList{
					Items: []corev1.Node{
						*nodeWithKubeletVersion("node-1", "v1.35.1"),
						*nodeWithKubeletVersion("node-1", "v1.36.2"),
						*nodeWithKubeletVersion("node-1", "v1.34.3"),
					},
				}, nil
			})

			minimumKubeletVersion, err := detectMinimumKubeletVersion(context.Background(), clientset, logd.Discard())

			Expect(err).NotTo(HaveOccurred())
			// the lowest version only occurs on the second chunk
			Expect(minimumKubeletVersion.Version.Major).To(Equal(1))
			Expect(minimumKubeletVersion.Version.Minor).To(Equal(34))
			Expect(listOptionsPerRequest).To(HaveLen(2))
			Expect(listOptionsPerRequest[0].Limit).To(Equal(int64(nodeListPageSize)))
			Expect(listOptionsPerRequest[0].Continue).To(BeEmpty())
			Expect(listOptionsPerRequest[1].Continue).To(Equal("second-chunk"))
		})
	})

	Describe("MinimumKubeletVersionDetector.StartDetection", func() {

		BeforeEach(func() {
			originalBackoff := minimumKubeletVersionDetectionBackoff
			minimumKubeletVersionDetectionBackoff = wait.Backoff{
				Duration: time.Millisecond,
				Factor:   1.0,
				Steps:    3,
			}
			DeferCleanup(func() {
				minimumKubeletVersionDetectionBackoff = originalBackoff
			})
		})

		It("should report the minimum kubelet version and notify the callback once detection succeeds", func() {
			clientset := fake.NewClientset(nodesWithKubeletVersions([]string{"v1.38.0", "v1.36.2", "v1.37.1"})...)
			detector := NewMinimumKubeletVersionDetector()
			onDetectedCalls := atomic.Int32{}

			detector.StartDetection(context.Background(), clientset, func() { onDetectedCalls.Add(1) }, logd.Discard())

			Eventually(func(g Gomega) {
				minimumKubeletVersion := detector.Get()
				g.Expect(minimumKubeletVersion.Detected).To(BeTrue())
				g.Expect(minimumKubeletVersion.Version.Major).To(Equal(1))
				g.Expect(minimumKubeletVersion.Version.Minor).To(Equal(36))
			}).Should(Succeed())
			Eventually(onDetectedCalls.Load).Should(Equal(int32(1)))
			Consistently(onDetectedCalls.Load).Should(Equal(int32(1)))
		})

		It("should keep reporting the version as undetected and never notify the callback when it gives up", func() {
			clientset := fake.NewClientset()
			listRequests := atomic.Int32{}
			clientset.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
				listRequests.Add(1)
				return true, nil, apierrors.NewInternalError(fmt.Errorf("the API server is unavailable"))
			})
			detector := NewMinimumKubeletVersionDetector()
			onDetectedCalls := atomic.Int32{}

			detector.StartDetection(context.Background(), clientset, func() { onDetectedCalls.Add(1) }, logd.Discard())

			Eventually(listRequests.Load).Should(
				Equal(int32(minimumKubeletVersionDetectionBackoff.Steps)))
			Consistently(func(g Gomega) {
				g.Expect(detector.Get().Detected).To(BeFalse())
				g.Expect(onDetectedCalls.Load()).To(BeZero())
				g.Expect(listRequests.Load()).To(Equal(int32(minimumKubeletVersionDetectionBackoff.Steps)))
			}).Should(Succeed())
		})

		It("should stop retrying without notifying the callback when the context is cancelled", func() {
			clientset := fake.NewClientset()
			clientset.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewInternalError(fmt.Errorf("the API server is unavailable"))
			})
			cancellableCtx, cancel := context.WithCancel(context.Background())
			cancel()
			detector := NewMinimumKubeletVersionDetector()
			onDetectedCalls := atomic.Int32{}

			detector.StartDetection(cancellableCtx, clientset, func() { onDetectedCalls.Add(1) }, logd.Discard())

			Consistently(func(g Gomega) {
				g.Expect(detector.Get().Detected).To(BeFalse())
				g.Expect(onDetectedCalls.Load()).To(BeZero())
			}).Should(Succeed())
		})
	})

	Describe("MinimumKubeletVersionDetector.WaitForDetection", func() {

		BeforeEach(func() {
			originalBackoff := minimumKubeletVersionDetectionBackoff
			minimumKubeletVersionDetectionBackoff = wait.Backoff{
				Duration: time.Millisecond,
				Factor:   1.0,
				Steps:    3,
			}
			DeferCleanup(func() {
				minimumKubeletVersionDetectionBackoff = originalBackoff
			})
		})

		It("should return the version once the detection has succeeded", func() {
			clientset := fake.NewClientset(nodesWithKubeletVersions([]string{"v1.38.0", "v1.36.2"})...)
			detector := NewMinimumKubeletVersionDetector()
			detector.StartDetection(context.Background(), clientset, nil, logd.Discard())

			minimumKubeletVersion := detector.WaitForDetection(context.Background(), 30*time.Second)

			Expect(minimumKubeletVersion.Detected).To(BeTrue())
			Expect(minimumKubeletVersion.Version.Minor).To(Equal(36))
		})

		It("should return an undetected version once the detection has given up, without waiting for the timeout",
			func() {
				clientset := fake.NewClientset()
				clientset.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewInternalError(fmt.Errorf("the API server is unavailable"))
				})
				detector := NewMinimumKubeletVersionDetector()
				detector.StartDetection(context.Background(), clientset, nil, logd.Discard())

				// A timeout far beyond the test's own budget: returning at all proves the wait ends when the detection
				// gives up, rather than when the timeout elapses.
				Expect(detector.WaitForDetection(context.Background(), time.Hour).Detected).To(BeFalse())
			})

		It("should give up waiting when the timeout elapses while the detection is still in progress", func() {
			detector := NewMinimumKubeletVersionDetector()

			// Detection is never started, so it never settles.
			Expect(detector.WaitForDetection(context.Background(), time.Millisecond).Detected).To(BeFalse())
		})

		It("should stop waiting when the context is done", func() {
			detector := NewMinimumKubeletVersionDetector()
			cancelledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			Expect(detector.WaitForDetection(cancelledCtx, time.Hour).Detected).To(BeFalse())
		})

		It("should report an undetected version for a nil detector", func() {
			var detector *MinimumKubeletVersionDetector

			Expect(detector.WaitForDetection(context.Background(), time.Hour).Detected).To(BeFalse())
		})
	})
})

// nodesWithKubeletVersions creates one node object per given kubelet version, for seeding a fake clientset.
func nodesWithKubeletVersions(kubeletVersions []string) []runtime.Object {
	nodes := make([]runtime.Object, 0, len(kubeletVersions))
	for i, kubeletVersion := range kubeletVersions {
		nodes = append(nodes, nodeWithKubeletVersion(fmt.Sprintf("node-%d", i), kubeletVersion))
	}
	return nodes
}

func nodeWithKubeletVersion(name string, kubeletVersion string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: kubeletVersion}},
	}
}
