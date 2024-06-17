package workloads_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorkloadModifications(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workload Modifications Suite")
}
