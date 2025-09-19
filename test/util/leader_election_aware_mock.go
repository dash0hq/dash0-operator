// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

type LeaderElectionAwareMock struct {
	isLeader bool
}

func NewLeaderElectionAwareMock(isLeader bool) *LeaderElectionAwareMock {
	return &LeaderElectionAwareMock{
		isLeader: isLeader,
	}
}

func (l *LeaderElectionAwareMock) NeedLeaderElection() bool {
	return true
}

func (l *LeaderElectionAwareMock) IsLeader() bool {
	return l.isLeader
}

func (l *LeaderElectionAwareMock) SetLeader(isLeader bool) {
	l.isLeader = isLeader
}
