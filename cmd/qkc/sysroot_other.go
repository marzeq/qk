//go:build !darwin

package main

import "runtime"

func hostDefaultTarget() (string, error) {
	return defaultTargetForHost(runtime.GOOS), nil
}

func hostAppleSysroot() (string, error) {
	return "", nil
}
