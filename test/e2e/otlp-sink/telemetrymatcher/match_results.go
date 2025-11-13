// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log"
)

// MatchResultList collects match results for a collection of potentially matching objects.
type MatchResultList[R any, O any] struct {
	objectResults []ObjectMatchResult[R, O]
}

const (
	numRepresentativeMatchesForOutput = 10
)

func newMatchResultList[R any, O any]() MatchResultList[R, O] {
	return MatchResultList[R, O]{
		objectResults: make([]ObjectMatchResult[R, O], 0),
	}
}

func (mrl *MatchResultList[R, O]) addResultForObject(matchResult ObjectMatchResult[R, O]) {
	mrl.objectResults = append(mrl.objectResults, matchResult)
}

func (mrl *MatchResultList[R, O]) hasMatch() bool {
	for _, omr := range mrl.objectResults {
		if omr.isMatch() {
			return true
		}
	}
	return false
}

func (mrl *MatchResultList[R, O]) expectAtLeastOneMatch(message string) ExpectationResult {
	var bestMatchScoreSoFar float32 = 0
	bestMatches := make([]ObjectMatchResult[R, O], 0)
	for _, omr := range mrl.objectResults {
		if omr.isMatch() {
			// We have a match, simply return from the function.
			return newSuccess()

		} else {
			matchScore := omr.matchScore()
			if matchScore > bestMatchScoreSoFar {
				bestMatchScoreSoFar = matchScore
				bestMatches = []ObjectMatchResult[R, O]{omr}
			} else if matchScore == bestMatchScoreSoFar {
				bestMatches = append(bestMatches, omr)
			}
		}
	}

	// We didn't find a match the rest of the method is about presenting the most likely matches.
	bestMatchesAsString := ""
	for i, omr := range bestMatches {
		bestMatchesAsString += fmt.Sprintf("\n--------\n%v", omr)
		if i >= numRepresentativeMatchesForOutput {
			break
		}
	}
	var description string
	if len(mrl.objectResults) > 0 && len(bestMatches) > 0 {
		description = fmt.Sprintf(
			"%s -- expected at least one matching object but none of the %d objects matched all expectations; here are "+
				"%d of %d best matches (with a match score of %d/100):\n%s",
			message,
			len(mrl.objectResults),
			numRepresentativeMatchesForOutput,
			len(bestMatches),
			int(bestMatchScoreSoFar*100),
			bestMatchesAsString,
		)
	} else if len(mrl.objectResults) > 0 {
		description = fmt.Sprintf(
			"%s -- expected at least one matching object but none of the %d objects matched all expectations. Also, "+
				"no best matching objects could be identified.",
			message,
			len(mrl.objectResults),
		)
	} else {
		description = fmt.Sprintf(
			"%s -- expected at least one matching object but no object of this signal type has been captured.",
			message,
		)
	}
	return newFailureWithDescription(description)
}

func (mrl *MatchResultList[R, O]) expectExactlyOneMatch(message string) ExpectationResult {
	atLeastOneResult := mrl.expectAtLeastOneMatch(message)
	if !atLeastOneResult.Success {
		return atLeastOneResult
	}

	numberOfMatches := mrl.numberOfMatches()
	if numberOfMatches == 1 {
		return newSuccess()
	}
	return newFailureWithDescription(fmt.Sprintf(
		"%s -- expected exactly one matching object but there were actually %d matching objects",
		message,
		numberOfMatches,
	))
}

func (mrl *MatchResultList[R, O]) expectZeroMatches(message string) ExpectationResult {
	matchingResults := make([]ObjectMatchResult[R, O], 0)
	for _, omr := range mrl.objectResults {
		if omr.isMatch() {
			matchingResults = append(matchingResults, omr)
		}
	}
	if len(matchingResults) == 0 {
		return newSuccess()
	} else {
		return newFailureWithDescription(
			fmt.Sprintf(
				"%s -- expected no matching objects but found %d matches, here is an arbitrary matching result:\n%s",
				message,
				len(matchingResults),
				matchingResults[0].String(),
			),
		)
	}
}

func (mrl *MatchResultList[R, O]) numberOfMatches() int {
	numMatches := 0
	for _, omr := range mrl.objectResults {
		if omr.isMatch() {
			numMatches++
		}
	}
	return numMatches
}

// ResourceMatchResult represents results for a single resource.
type ResourceMatchResult[R any] struct {
	resource          R
	assertionOutcomes []AssertionOutcome
}

func newResourceMatchResult[R any](resource R) ResourceMatchResult[R] {
	return ResourceMatchResult[R]{
		resource: resource,
	}
}

func (rmr *ResourceMatchResult[R]) addPassedAssertion(id string) {
	rmr.assertionOutcomes = append(rmr.assertionOutcomes, newAssertionOutcome(id, assertionPassed, ""))
}

func (rmr *ResourceMatchResult[R]) addFailedAssertion(id string, message string) {
	rmr.assertionOutcomes = append(rmr.assertionOutcomes, newAssertionOutcome(id, assertionFailed, message))
}

func (rmr *ResourceMatchResult[R]) addSkippedAssertion(id string, message string) {
	rmr.assertionOutcomes = append(rmr.assertionOutcomes, newAssertionOutcome(id, assertionSkipped, message))
}

// ObjectMatchResult represents results for one potentially matching object.
type ObjectMatchResult[R any, O any] struct {
	name              string
	resource          R
	object            O
	assertionOutcomes []AssertionOutcome
}

func newObjectMatchResult[R any, O any](
	name string,
	resource R,
	resourceMatchResult ResourceMatchResult[R],
	object O,
) ObjectMatchResult[R, O] {
	omr := ObjectMatchResult[R, O]{
		name:              name,
		resource:          resource,
		object:            object,
		assertionOutcomes: make([]AssertionOutcome, 0),
	}
	omr.assertionOutcomes = append(omr.assertionOutcomes, resourceMatchResult.assertionOutcomes...)
	return omr
}

func (omr *ObjectMatchResult[R, O]) addPassedAssertion(id string) {
	omr.assertionOutcomes = append(omr.assertionOutcomes, newAssertionOutcome(id, assertionPassed, ""))
}

func (omr *ObjectMatchResult[R, O]) addFailedAssertion(id string, message string) {
	omr.assertionOutcomes = append(omr.assertionOutcomes, newAssertionOutcome(id, assertionFailed, message))
}

func (omr *ObjectMatchResult[R, O]) addSkippedAssertion(id string, message string) {
	omr.assertionOutcomes = append(omr.assertionOutcomes, newAssertionOutcome(id, assertionSkipped, message))
}

func (omr *ObjectMatchResult[R, O]) isMatch() bool {
	if len(omr.assertionOutcomes) == 0 {
		log.Printf("warning: isMatch called with empty assertion outcomes list")
		return false
	}
	for _, assertionOutcome := range omr.assertionOutcomes {
		if !assertionOutcome.ok() {
			return false
		}
	}
	return true
}

func (omr *ObjectMatchResult[R, O]) matchScore() float32 {
	passedChecks := 0
	for _, assertionOutcome := range omr.assertionOutcomes {
		if assertionOutcome.ok() {
			passedChecks++
		}
	}
	return float32(passedChecks) / float32(len(omr.assertionOutcomes))
}

func (omr ObjectMatchResult[R, O]) String() string {
	s := omr.name + ":"
	for _, ao := range omr.assertionOutcomes {
		s += fmt.Sprintf("\n%s", ao.String())
	}
	return s
}

type AssertionOutcome struct {
	id      string
	result  AssertionResult
	message string
}

func newAssertionOutcome(id string, result AssertionResult, message string) AssertionOutcome {
	return AssertionOutcome{
		id:      id,
		result:  result,
		message: message,
	}
}

func (ao *AssertionOutcome) ok() bool {
	return ao.result.ok()
}

func (ao AssertionOutcome) String() string {
	resultStr := ao.result.String()
	if ao.result == assertionFailed {
		resultStr = "! FAILED"
	}
	if ao.message != "" {
		return fmt.Sprintf("- %s: %s - %s", ao.id, resultStr, ao.message)
	}
	return fmt.Sprintf("- %s: %s ", ao.id, resultStr)
}

type AssertionResult int

const (
	assertionPassed = iota
	assertionFailed
	assertionSkipped
)

func (ar *AssertionResult) ok() bool {
	return *ar == assertionPassed || *ar == assertionSkipped
}

func (ar AssertionResult) String() string {
	switch ar {
	case assertionPassed:
		return "passed"
	case assertionFailed:
		return "failed"
	case assertionSkipped:
		return "skipped"
	default:
		return "UNKNOWN"
	}
}
