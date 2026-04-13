//go:build linux

// Package main is required for buildmode=c-shared.
// This file exists solely to satisfy the Go toolchain requirement
// that a c-shared library must have an empty main function.
package main

func main() {}
