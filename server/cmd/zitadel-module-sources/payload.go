// Command zitadel-module-sources retains verified selected Go module inputs.
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

// verifyPayload checks a retained module archive and its separate go.mod.
func verifyPayload(path, version, zipFile string, mod []byte, sum, modSum string) error {
	if err := module.Check(path, version); err != nil {
		return fmt.Errorf("module identity: %w", err)
	}
	if _, err := modzip.CheckZip(module.Version{Path: path, Version: version}, zipFile); err != nil {
		return fmt.Errorf("module ZIP structure: %w", err)
	}
	archive, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	for _, member := range archive.File {
		if mode := member.Mode(); !mode.IsRegular() && !mode.IsDir() {
			return errors.Join(fmt.Errorf("module ZIP contains a special member"), archive.Close())
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	actual, err := dirhash.HashZip(zipFile, dirhash.Hash1)
	if err != nil || actual != sum {
		return fmt.Errorf("module ZIP content checksum differs: %v", err)
	}
	actual, err = dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(mod)), nil
	})
	if err != nil || actual != modSum {
		return fmt.Errorf("module go.mod checksum differs: %v", err)
	}
	return nil
}
