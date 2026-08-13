// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import "strings"

// parseArguments resolves the argument list of a kubectl invocation into its subcommand, its flags and the resource
// types it references. This is the only place that walks an argument list, so that the value of a value-taking flag is
// consistently recognized as such: it is neither a positional argument (the "foo" in `kubectl -n foo get pods` is
// neither the subcommand nor a resource type) nor a flag of its own when it starts with a dash (the "-1" in
// `kubectl logs my-pod --tail -1`).
func parseArguments(arguments []string) parsedArguments {
	parsed := parsedArguments{}
	// positionalIndex counts the positional arguments that follow the subcommand; it stays negative until the subcommand
	// itself has been found.
	positionalIndex := -1

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]

		if !strings.HasPrefix(argument, "-") {
			if positionalIndex < 0 {
				parsed.subcommand = argument
				positionalIndex = 0
				continue
			}
			parsed.resourceTypes =
				append(parsed.resourceTypes, extractNormalizedResourceTypes(argument, positionalIndex == 0)...)
			positionalIndex++
			continue
		}

		flag, consumesNextArg, allowed := inspectFlagToken(argument)
		if !allowed {
			parsed.disallowedFlags = append(parsed.disallowedFlags, argument)
			continue
		}
		if consumesNextArg && i+1 < len(arguments) {
			flag.value = arguments[i+1]
			i++ // the flag's value is neither a positional argument nor a flag of its own
		}
		parsed.flags = append(parsed.flags, flag)
	}

	return parsed
}

// extractNormalizedResourceTypes returns the normalized resource types a single positional argument references. The
// argument may be a comma-separated list of resources (e.g. "secret,configmap"), and each entry either a bare resource
// type or a type/name pair (e.g. "secret/my-secret"). A bare resource type only denotes a resource type in the first
// positional argument after the subcommand (the resource type slot, isResourceTypeSlot), while a type/name pair does so
// in any slot, since `kubectl get secret/a pod/b` lists multiple pairs.
func extractNormalizedResourceTypes(argument string, isResourceTypeSlot bool) []string {
	var resourceTypes []string
	for _, part := range strings.Split(argument, ",") {
		resourceType, _, isTypeNamePair := strings.Cut(part, "/")
		if !isTypeNamePair && !isResourceTypeSlot {
			continue
		}
		resourceTypes = append(resourceTypes, normalizeResourceType(resourceType))
	}
	return resourceTypes
}

// normalizeResourceType lower-cases a resource type and strips the API group/version suffix of fully qualified forms:
// "secrets.v1." -> "secrets", "Dash0Monitorings.operator.dash0.com" -> "dash0monitorings".
func normalizeResourceType(resourceType string) string {
	resourceType = strings.ToLower(resourceType)
	if idx := strings.Index(resourceType, "."); idx >= 0 {
		resourceType = resourceType[:idx]
	}
	return resourceType
}

// inspectFlagToken resolves a token starting with "-" against the allowedFlags allowlist. A "--flag" token references
// one flag, while a "-abc" token may group several shorthands (pflag accepts "-Aw"), where a value-taking shorthand
// takes the rest of the token as its value, or the following argument when nothing follows it. A bare "--" or "-"
// references no flag at all and is not allowed.
//
// It reports consumesNextArg when the token's value-taking flag expects the following argument as its value, and
// allowed when every flag the token references is in the allowlist. The returned flag is only meaningful when the token
// is allowed.
func inspectFlagToken(token string) (flag parsedFlag, consumesNextArg bool, allowed bool) {
	if nameAndValue, isLongFlag := strings.CutPrefix(token, "--"); isLongFlag {
		name, inlineValue, hasInlineValue := strings.Cut(nameAndValue, "=")
		takesValue, isAllowed := allowedFlags[name]
		if !isAllowed {
			return parsedFlag{}, false, false
		}
		flag = parsedFlag{token: token}
		if takesValue {
			flag.valueTakingName = name
			flag.value = inlineValue
		} else {
			// The value of a boolean flag is not tracked, the flag counts as referenced either way.
			flag.booleanNames = []string{name}
		}
		return flag, takesValue && !hasInlineValue, true
	}

	shorthands := strings.TrimPrefix(token, "-")
	if shorthands == "" {
		return parsedFlag{}, false, false
	}
	flag = parsedFlag{token: token}
	for i, shorthand := range []byte(shorthands) {
		name := string(shorthand)
		takesValue, isAllowed := allowedFlags[name]
		if !isAllowed {
			return parsedFlag{}, false, false
		}
		if !takesValue {
			flag.booleanNames = append(flag.booleanNames, name)
			continue
		}
		// Whatever follows the shorthand is its value, written as "-nx" or "-n=x"; an empty remainder means the value is
		// the following argument.
		inlineValue, hasEqualsSign := strings.CutPrefix(shorthands[i+1:], "=")
		flag.valueTakingName = name
		flag.value = inlineValue
		return flag, inlineValue == "" && !hasEqualsSign, true
	}
	return flag, false, true
}
