// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"regexp"
	"strconv"

	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
)

var (
	sequenceOfMappingsRegex = regexp.MustCompile(`^([\w-]+)=([\w-]+)$`)
	sequenceIndexRegex      = regexp.MustCompile(`^(\d+)$`)
)

func ParseConfigMapContent(configMap *corev1.ConfigMap, dataKey string) map[string]any {
	configMapContent := configMap.Data[dataKey]
	configMapParsed := &map[string]any{}
	err := yaml.Unmarshal([]byte(configMapContent), configMapParsed)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Cannot parse config map content:\n%s\n", configMapContent))
	return *configMapParsed
}

//nolint:lll
func ReadFromMap(object any, path []string) any {
	key := path[0]
	var sub any

	sequenceOfMappingsMatches := sequenceOfMappingsRegex.FindStringSubmatch(key)
	sequenceIndexMatches := sequenceIndexRegex.FindStringSubmatch(key)
	if len(sequenceOfMappingsMatches) > 0 {
		// assume we have a sequence of objects, read by equality comparison with an attribute

		attributeName := sequenceOfMappingsMatches[1]
		attributeValue := sequenceOfMappingsMatches[2]

		s, isSlice := object.([]any)
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key \"%s\", got %T", key, object))
		for _, item := range s {
			m, isMapInSlice := item.(map[string]any)
			Expect(isMapInSlice).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when checking an item in the slice read via key \"%s\", got %T", key, object))
			val := m[attributeName]
			if val == attributeValue {
				sub = item
				break
			}
		}
	} else if len(sequenceIndexMatches) > 0 {
		// assume we have an indexed sequence, read by index
		indexRaw := sequenceIndexMatches[1]
		index, err := strconv.Atoi(indexRaw)
		Expect(err).ToNot(HaveOccurred())
		s, isSlice := object.([]any)
		Expect(isSlice).To(BeTrue(), fmt.Sprintf("expected a []interface{} when reading key \"%s\", got %T", key, object))
		Expect(len(s) > index).To(BeTrue())
		sub = s[index]
	} else {
		// assume we have a regular map, read by key
		m, isMap := object.(map[string]any)
		Expect(isMap).To(BeTrue(), fmt.Sprintf("expected a map[string]interface{} when reading key \"%s\", got %T", key, object))
		sub = m[key]
	}

	if len(path) == 1 {
		return sub
	}
	Expect(sub).ToNot(BeNil(), fmt.Sprintf("expected a nested element to be not nil when reading key \"%s\" in object %v", key, object))
	return ReadFromMap(sub, path[1:])
}
