// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/collector/pdata/pprofile"
)

const (
	profilesJsonMaxLineLength = 1_048_576
)

var (
	profileUnmarshaller = &pprofile.JSONUnmarshaler{}
)

//nolint:dupl
func readFileAndGetMatchingProfiles(
	profilesJsonlFilename string,
	resourceMatchFn func(pprofile.ResourceProfiles, *ResourceMatchResult[pprofile.ResourceProfiles]),
	profileMatchFn func(pprofile.Profile, *ObjectMatchResult[pprofile.ResourceProfiles, pprofile.Profile]),
	timestampLowerBound time.Time,
) (*MatchResultList[pprofile.ResourceProfiles, pprofile.Profile], error) {
	fileHandle, err := os.Open(profilesJsonlFilename)
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %v", profilesJsonlFilename, err)
	}
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, profilesJsonMaxLineLength), profilesJsonMaxLineLength)

	matchResults := newMatchResultList[pprofile.ResourceProfiles, pprofile.Profile]()

	for scanner.Scan() {
		resourceProfileBytes := scanner.Bytes()
		profiles, err := profileUnmarshaller.UnmarshalProfiles(resourceProfileBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		extractMatchingProfiles(
			profiles,
			resourceMatchFn,
			profileMatchFn,
			timestampLowerBound,
			&matchResults,
		)
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("error while scanning file %s: %v", profilesJsonlFilename, err)
	}

	return &matchResults, nil
}

func extractMatchingProfiles(
	profiles pprofile.Profiles,
	resourceMatchFn func(pprofile.ResourceProfiles, *ResourceMatchResult[pprofile.ResourceProfiles]),
	profileMatchFn func(pprofile.Profile, *ObjectMatchResult[pprofile.ResourceProfiles, pprofile.Profile]),
	timestampLowerBound time.Time,
	allMatchResults *MatchResultList[pprofile.ResourceProfiles, pprofile.Profile],
) {
	for i := 0; i < profiles.ResourceProfiles().Len(); i++ {
		resourceProfile := profiles.ResourceProfiles().At(i)
		resourceMatchResult := newResourceMatchResult(resourceProfile)
		if resourceMatchFn != nil {
			resourceMatchFn(resourceProfile, &resourceMatchResult)
		}

		for j := 0; j < resourceProfile.ScopeProfiles().Len(); j++ {
			scopeProfile := resourceProfile.ScopeProfiles().At(j)
			for k := 0; k < scopeProfile.Profiles().Len(); k++ {
				profile := scopeProfile.Profiles().At(k)
				profileMatchResult := newObjectMatchResult(
					fmt.Sprintf("profile-%d-%d-%d", i, j, k),
					resourceProfile,
					resourceMatchResult,
					profile,
				)
				profileMatchFn(profile, &profileMatchResult)
				if profile.Time().AsTime().Before(timestampLowerBound) {
					if profileMatchResult.isMatch() {
						log.Printf(
							"Ignoring matching profile because of timestamp: lower bound: %s (%d) vs. profile: %s (%d)",
							timestampLowerBound.String(),
							timestampLowerBound.UnixNano(),
							profile.Time().String(),
							profile.Time().AsTime().UnixNano(),
						)
					}
					continue
				}
				allMatchResults.addResultForObject(profileMatchResult)
			}
		}
	}
}

// matchAnyProfileMatcher matches any profile unconditionally.
func matchAnyProfileMatcher() func(
	pprofile.Profile,
	*ObjectMatchResult[pprofile.ResourceProfiles, pprofile.Profile],
) {
	return func(_ pprofile.Profile, matchResult *ObjectMatchResult[pprofile.ResourceProfiles, pprofile.Profile]) {
		matchResult.addPassedAssertion("profile-exists")
	}
}

// profilesResourceMatcher validates that profile resource attributes contain Kubernetes metadata enriched by the
// resourcedetection and k8s_attributes processors in the operator's collector pipeline.
func profilesResourceMatcher() func(
	pprofile.ResourceProfiles,
	*ResourceMatchResult[pprofile.ResourceProfiles],
) {
	return func(resourceProfiles pprofile.ResourceProfiles, matchResult *ResourceMatchResult[pprofile.ResourceProfiles]) {
		resourceAttributes := resourceProfiles.Resource().Attributes()

		// Added by the resourcedetection processor (k8snode detector).
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.node.name",
			matchResult,
		)

		// Added by the k8s_attributes processor via pod association (from: connection).
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.namespace.name",
			matchResult,
		)
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.pod.name",
			matchResult,
		)
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.pod.uid",
			matchResult,
		)
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.daemonset.name",
			matchResult,
		)

		// Added by the k8s_attributes processor: cluster-level metadata.
		verifyResourceAttributeExists[pprofile.ResourceProfiles](
			resourceAttributes,
			"k8s.cluster.uid",
			matchResult,
		)
	}
}
