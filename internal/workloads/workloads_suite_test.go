package workloads_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestK8sresources(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workload Modifications Suite")
}
