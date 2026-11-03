//go:build integration

package customeridentity

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMaterialProjectionContainerArchive retains a real child directory's metadata at Docker extraction.
func TestMaterialProjectionContainerArchive(t *testing.T) {
	for _, phase := range []string{"init", "setup", "serve"} {
		t.Run(phase, func(t *testing.T) {
			input, _ := materialProjectionFixture(t, phase)
			projectionMust(t, os.Chmod(input, 0777))
			data := projectionInputArchive(t, input)
			header, err := tar.NewReader(bytes.NewReader(data)).Next()
			projectionMust(t, err)
			// Extraction deliberately ignores metadata on an existing destination's dot entry.
			if header.Name != "input" || header.Typeflag != tar.TypeDir || header.Mode != 0777 || header.Uid != 0 || header.Gid != 65532 {
				t.Fatal("archive lacks the independently owned projection directory")
			}
			projectionAssertCopied(t, input, data)
		})
	}
}

// TestMaterialProjectionContainer executes the actual packaged non-root application on Linux mounts.
func TestMaterialProjectionContainer(t *testing.T) {
	image := os.Getenv("PORTAL_IDENTITY_CONTAINER_IMAGE")
	if image != "business-site:test" && image != "business-site:production-test" {
		t.Fatal("select a locally built source-test image")
	}
	for _, phase := range []string{"init", "setup", "serve"} {
		input, _ := materialProjectionFixture(t, phase)
		projectionMust(t, os.Chmod(input, 0777))
		archive := projectionInputArchive(t, input)
		volumeBytes, err := projectionDocker(nil, "volume", "create")
		projectionMust(t, err)
		volume := strings.TrimSpace(string(volumeBytes))
		if len(volume) != 64 || strings.Trim(volume, "0123456789abcdef") != "" {
			t.Fatal("invalid owned volume identity")
		}
		t.Cleanup(func() {
			if _, e := projectionDocker(nil, "volume", "rm", volume); e != nil {
				t.Error("owned identity volume cleanup failed")
			}
		})
		carrierBytes, err := projectionDocker(nil, "create", "--network=none", "--mount", "type=volume,source="+volume+",target=/projection", image, "--version")
		projectionMust(t, err)
		carrier := strings.TrimSpace(string(carrierBytes))
		if len(carrier) != 64 || strings.Trim(carrier, "0123456789abcdef") != "" {
			t.Fatal("invalid owned carrier identity")
		}
		t.Cleanup(func() {
			if carrier != "" {
				if _, e := projectionDocker(nil, "rm", carrier); e != nil {
					t.Error("owned identity carrier cleanup failed")
				}
			}
		})
		// The carrier never starts. Copying an archive applies numeric ownership without a privileged process.
		if _, err := projectionDocker(archive, "cp", "-", carrier+":/projection"); err != nil {
			t.Fatal("identity fixture copy failed")
		}
		actual, err := projectionDocker(nil, "cp", carrier+":/projection/input", "-")
		projectionMust(t, err)
		projectionAssertCopied(t, input, actual)
		if _, err := projectionDocker(nil, "rm", carrier); err != nil {
			t.Fatal("owned identity carrier cleanup failed")
		}
		carrier = ""
		for _, readOnly := range []bool{true, false} {
			mode := "writable"
			mount := "type=volume,source=" + volume + ",target=/projection"
			if readOnly {
				mode = "read-only"
				mount += ",readonly"
			}
			t.Run(phase+"/"+mode, func(t *testing.T) {
				args := []string{"run", "--rm", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=256m", "--cpus=1", "--mount", mount, "--tmpfs", "/output:rw,noexec,nosuid,nodev,size=1m,uid=65532,gid=65532,mode=0700", image, "--identity-materialize", "--identity-phase", phase, "--identity-projection", "/projection/input", "--identity-output", "/output/phase"}
				output, err := projectionDocker(nil, args...)
				if readOnly {
					if err != nil || string(output) != "customer identity phase prepared\n" {
						t.Fatal("actual restricted image refused the kernel read-only projection")
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(output) != 0 || string(exit.Stderr) != "customer identity phase preparation refused\n" {
						t.Fatal("actual restricted image accepted or misreported the writable projection")
					}
				}
			})
		}
	}
}

// projectionInputArchive preserves the closed fictional fixture with explicit Linux ownership.
func projectionInputArchive(t *testing.T, input string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	err := filepath.WalkDir(input, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(filename)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name, err = filepath.Rel(input, filename)
		if err != nil {
			return err
		}
		// A named child is created with these attributes; Docker ignores an existing dot root.
		header.Name = filepath.Join("input", header.Name)
		header.Uid, header.Gid = 0, 65532
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		_, err = writer.Write(contents)
		return err
	})
	projectionMust(t, err)
	projectionMust(t, writer.Close())
	return data.Bytes()
}

// projectionAssertCopied reads actual daemon archive bytes and metadata before executing the image.
func projectionAssertCopied(t *testing.T, input string, data []byte) {
	t.Helper()
	expectedMembers := map[string]os.FileInfo{}
	projectionMust(t, filepath.WalkDir(input, func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(input, filename)
		if err != nil {
			return err
		}
		expectedMembers[name], err = entry.Info()
		return err
	}))
	reader := tar.NewReader(bytes.NewReader(data))
	observed := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		projectionMust(t, err)
		if header.Name != "input" && header.Name != "input/" && !strings.HasPrefix(header.Name, "input/") {
			t.Fatal("copied fixture member is outside its named root")
		}
		name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "input/"), "/")
		if name == "input" || name == "" {
			name = "."
		}
		if observed[name] {
			t.Fatal("duplicate copied fixture member")
		}
		info, allowed := expectedMembers[name]
		if !allowed {
			t.Fatal("unexpected copied fixture member")
		}
		observed[name] = true
		if header.Uid != 0 || header.Gid != 65532 || header.FileInfo().Mode()&os.ModeType != info.Mode()&os.ModeType || os.FileMode(header.Mode).Perm() != info.Mode().Perm() {
			t.Fatal("actual copied fixture lost its ownership or mode")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(filepath.Join(input, name))
			projectionMust(t, e)
			if header.Linkname != target {
				t.Fatal("copied fixture link target differs")
			}
		}
		if info.Mode().IsRegular() {
			actual, e := io.ReadAll(io.LimitReader(reader, 65537))
			projectionMust(t, e)
			expected, e := os.ReadFile(filepath.Join(input, name))
			projectionMust(t, e)
			if !bytes.Equal(actual, expected) {
				t.Fatal("copied fictional input bytes differ")
			}
		}
	}
	if len(observed) != len(expectedMembers) {
		t.Fatal("copied fixture membership is incomplete")
	}
}

// projectionDocker bounds owned disposable operations; no shell or helper image is executed.
func projectionDocker(input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", args...)
	command.WaitDelay = time.Second
	command.Stdin = bytes.NewReader(input)
	return command.Output()
}
