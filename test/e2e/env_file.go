// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	isCommentRegexp = regexp.MustCompile(`^\s*#`)
)

func readDotEnvFile(filename string) (map[string]string, error) {
	env := map[string]string{}

	if len(filename) == 0 {
		return nil, fmt.Errorf("filename is empty")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf(
			"the .env file (%s) is missing or cannot be opened; please copy the file .env.template and edit the "+
				"values according to your local environment. %w", filename, err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if isCommentRegexp.MatchString(line) {
			continue
		}
		if equalSignIdx := strings.Index(line, "="); equalSignIdx >= 0 {
			if key := strings.TrimSpace(line[:equalSignIdx]); len(key) > 0 {
				value := ""
				if len(line) > equalSignIdx {
					value = strings.TrimSpace(line[equalSignIdx+1:])
				}
				env[key] = value
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	return env, nil
}
