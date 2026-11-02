package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strings"
)

type sourceLayerFile struct {
	Data      []byte
	ImageMode uint32
}
type sourceLayerResult struct {
	Files         map[string]sourceLayerFile
	ExpandedBytes int64
}

// This reads selected original data from a hash-bound base layer. It never
// extracts an archive, follows its links, runs a program or changes a filesystem.
func checkBaseLayer(blob []byte, spec sourceObject, diffID string, wanted []sourceDerived) (sourceLayerResult, error) {
	var result sourceLayerResult
	if int64(len(blob)) != spec.Bytes || fmt.Sprintf("%x", sha256.Sum256(blob)) != spec.SHA256 {
		return result, fmt.Errorf("compressed base layer differs")
	}
	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return result, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(gz, (8<<20)+1))
	closeErr := gz.Close()
	if readErr != nil || closeErr != nil || len(raw) > 8<<20 {
		return result, fmt.Errorf("incomplete or oversized base layer")
	}
	if fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != diffID {
		return result, fmt.Errorf("uncompressed base DiffID differs")
	}
	selected := map[string]sourceDerived{}
	status := map[string]bool{}
	for _, v := range wanted {
		if strings.HasPrefix(v.ImagePath, "var/lib/dpkg/status.d/") {
			status[v.ImagePath] = true
		}
		if v.LayerSHA256 == spec.SHA256 {
			selected[v.ImagePath] = v
		}
	}
	result.Files = map[string]sourceLayerFile{}
	result.ExpandedBytes = int64(len(raw))
	reader := tar.NewReader(bytes.NewReader(raw))
	members := 0
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return sourceLayerResult{}, err
		}
		members++
		if members > 10000 {
			return sourceLayerResult{}, fmt.Errorf("base layer member bound")
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "." || name == "" {
			if h.Typeflag == tar.TypeDir {
				continue
			}
			return sourceLayerResult{}, fmt.Errorf("empty base member")
		}
		if len(name) > 1024 || path.Clean(name) != name || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\n\r\t") || strings.HasPrefix(path.Base(name), ".wh.") {
			return sourceLayerResult{}, fmt.Errorf("unsafe or whiteout base member")
		}
		if strings.HasPrefix(name, "var/lib/dpkg/status.d/") && h.Typeflag != tar.TypeDir && !strings.HasSuffix(name, ".md5sums") && !status[name] {
			return sourceLayerResult{}, fmt.Errorf("unrecorded installed base package")
		}
		want, ok := selected[name]
		if !ok {
			continue
		}
		// tar.Reader normalizes legacy NUL regular headers to TypeReg.
		if _, seen := result.Files[name]; seen || h.Typeflag != tar.TypeReg || h.Size != want.Bytes || h.Mode != int64(want.ImageMode) {
			return sourceLayerResult{}, fmt.Errorf("selected base member type, size or mode differs")
		}
		b, err := io.ReadAll(io.LimitReader(reader, want.Bytes+1))
		if err != nil || int64(len(b)) != want.Bytes || fmt.Sprintf("%x", sha256.Sum256(b)) != want.SHA256 {
			return sourceLayerResult{}, fmt.Errorf("selected base member bytes differ")
		}
		result.Files[name] = sourceLayerFile{Data: b, ImageMode: uint32(h.Mode)}
	}
	if len(result.Files) != len(selected) {
		return sourceLayerResult{}, fmt.Errorf("selected base members missing")
	}
	return result, nil
}
