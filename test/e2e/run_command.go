// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/gomega"
)

func runAndIgnoreOutput(cmd *exec.Cmd, logCommandArgs ...bool) error {
	_, err := run(cmd, logCommandArgs...)
	return err
}

// run executes the provided command within this context
func run(cmd *exec.Cmd, logCommandArgs ...bool) (string, error) {
	var logCommand bool
	var alwaysLogOutput bool
	if len(logCommandArgs) >= 1 {
		logCommand = logCommandArgs[0]
	} else {
		logCommand = true
	}
	if len(logCommandArgs) >= 2 {
		alwaysLogOutput = logCommandArgs[1]
	} else {
		alwaysLogOutput = false
	}

	dir, _ := getProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		e2ePrint("chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	if logCommand {
		e2ePrint("running: %s\n", command)
	}
	output, err := cmd.CombinedOutput()
	if alwaysLogOutput {
		e2ePrint("output: %s\n", string(output))
	}
	if err != nil {
		return string(output), fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return string(output), nil
}

// getNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func getNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// getProjectDir will return the directory where the project is
func getProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

func verifyCommandOutputContainsStrings(command *exec.Cmd, timeout time.Duration, needles ...string) {
	Eventually(func(g Gomega) {
		// We cannot run the same exec.Command multiple times, thus we create a new instance on each attempt instead.
		haystack, err := run(exec.Command(command.Args[0], command.Args[1:]...), false)
		g.Expect(err).ToNot(HaveOccurred())
		for _, needle := range needles {
			g.Expect(haystack).To(ContainSubstring(needle))
		}
	}, timeout, time.Second).Should(Succeed())
}

func verifyCommandOutputDoesNotContainStrings(command *exec.Cmd, needles ...string) {
	haystack, err := run(exec.Command(command.Args[0], command.Args[1:]...), false)
	Expect(err).ToNot(HaveOccurred())
	for _, needle := range needles {
		Expect(haystack).ToNot(ContainSubstring(needle))
	}
}
