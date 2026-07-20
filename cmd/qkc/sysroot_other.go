//go:build !darwin

package main

func hostAppleSysroot() (string, error) {
	return "", nil
}
