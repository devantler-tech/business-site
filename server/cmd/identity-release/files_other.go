//go:build !darwin && !linux

// Package main implements explicit frozen identity source operations.
package main

import (
	"fmt"
	"os"
)

// No unverified link-count fallback on unsupported collector hosts.
func sourceSingleLink(os.FileInfo) bool { return false }

func sourceOpenRegular(*os.Root, string) (*os.File, error) {
	return nil, fmt.Errorf("verified nofollow/nonblocking source open unsupported on this host")
}
