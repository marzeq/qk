//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func hostDefaultTarget() (string, error) {
	// Use the running macOS version rather than the triple baked into libLLVM.
	version := os.Getenv("MACOSX_DEPLOYMENT_TARGET")
	if version == "" {
		output, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
		if err != nil {
			return "", fmt.Errorf("could not discover the macOS deployment target: %w", err)
		}
		version = strings.TrimSpace(string(output))
		if version == "" {
			return "", fmt.Errorf("sw_vers returned an empty macOS version")
		}
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	return arch + "-apple-macosx" + version, nil
}

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
