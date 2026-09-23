//go:build !darwin || !cgo

package main

func darwinLogoffAvailable() bool { return false }
