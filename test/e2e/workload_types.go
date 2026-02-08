// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

type workloadType struct {
	workloadTypeString          string
	workloadTypeStringCamelCase string
	isBatch                     bool
}

var (
	workloadTypeCronjob = workloadType{
		workloadTypeString:          "cronjob",
		workloadTypeStringCamelCase: "CronJob",
		isBatch:                     true,
	}
	workloadTypeDaemonSet = workloadType{
		workloadTypeString:          "daemonset",
		workloadTypeStringCamelCase: "DaemonSet",
		isBatch:                     false,
	}
	workloadTypeDeployment = workloadType{
		workloadTypeString:          "deployment",
		workloadTypeStringCamelCase: "Deployment",
		isBatch:                     false,
	}
	workloadTypeJob = workloadType{
		workloadTypeString:          "job",
		workloadTypeStringCamelCase: "Job",
		isBatch:                     true,
	}
	workloadTypePod = workloadType{
		workloadTypeString:          "pod",
		workloadTypeStringCamelCase: "Pod",
		isBatch:                     false,
	}
	workloadTypeReplicaSet = workloadType{
		workloadTypeString:          "replicaset",
		workloadTypeStringCamelCase: "ReplicaSet",
		isBatch:                     false,
	}
	workloadTypeStatefulSet = workloadType{
		workloadTypeString:          "statefulset",
		workloadTypeStringCamelCase: "StatefulSet",
		isBatch:                     false,
	}
)
