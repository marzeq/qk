//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func hostAppleSysroot() (string, error) {
	output, err := exec.Command("xcrun", "--sdk", "macosx", "--show-sdk-path").Output()
	if err != nil {
		return "", fmt.Errorf("could not discover the macOS SDK with xcrun: %w", err)
	}
	sysroot := strings.TrimSpace(string(output))
	if sysroot == "" {
		return "", fmt.Errorf("xcrun returned an empty macOS SDK path")
	}
	return sysroot, nil
}
