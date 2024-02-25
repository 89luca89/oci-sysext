// Package fileutils contains utilities and helpers to manage and manipulate files.
package fileutils

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/89luca89/oci-sysext/pkg/logging"
)

// ReadFile will return the content of input file or error.
// This is a linux-only implementation using syscalls for performance benefits.
func ReadFile(path string) ([]byte, error) {
	var stat syscall.Stat_t

	// ensure that file exists
	err := syscall.Stat(path, &stat)
	if err != nil {
		return nil, err
	}

	// and that I can open it
	fd, err := syscall.Open(path, syscall.O_RDONLY, uint32(os.ModePerm))
	if err != nil {
		logging.LogDebug("%v", err)

		return nil, err
	}

	defer func() { _ = syscall.Close(fd) }()

	fileLenght := 10000
	if stat.Size > 0 {
		fileLenght = int(stat.Size)
	}

	filedata := make([]byte, fileLenght)

	_, err = syscall.Read(fd, filedata)
	if err != nil {
		logging.LogError("%v", err)

		return nil, err
	}

	return filedata, nil
}

// WriteFile will write the content in input to file in path or error.
// This is a linux-only implementation using syscalls for performance benefits.
func WriteFile(path string, content []byte, perm uint32) error {
	var fd int

	var stat syscall.Stat_t
	// ensure that file exists
	err := syscall.Stat(path, &stat)
	if err != nil {
		fd, err = syscall.Creat(path, perm)
		if err != nil {
			logging.LogError("%v", err)

			return err
		}
	} else {
		fd, err = syscall.Open(path, syscall.O_RDWR, perm)
		if err != nil {
			logging.LogError("%v", err)

			return err
		}

		err = syscall.Chmod(path, perm)
		if err != nil {
			logging.LogError("%v", err)

			return err
		}
	}

	defer func() { _ = syscall.Close(fd) }()

	_, err = syscall.Write(fd, content)

	return err
}

// GetFileDigest will return the sha256sum of input file. Empty if error occurs.
func GetFileDigest(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}

	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return ""
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))
}

// CheckFileDigest will compare input digest to the checksum of input file.
// Returns whether the input digest is equal to the input file's one.
func CheckFileDigest(path string, digest string) bool {
	checksum := GetFileDigest(path)

	logging.LogDebug("input checksum is: %s", "sha256:"+checksum)
	logging.LogDebug("expected checksum is: %s", digest)

	return "sha256:"+checksum == digest
}

// Exist returns if a path exists or not.
func Exist(path string) bool {
	var stat syscall.Stat_t
	err := syscall.Stat(path, &stat)

	return err == nil
}

// UntarFile will untar target file to target directory.
// If userns is specified and it is keep-id, it will perform the
// untarring in a new user namespace with user id maps set, in order to prevent
// permission errors.
func UntarFile(path string, target string) error {
	// first ensure we can write
	err := syscall.Access(path, 2)
	if err != nil {
		logging.LogError("%v", err)

		return err
	}

	cmd := exec.Command("tar", "--exclude=dev/*", "-xf", path, "-C", target)
	logging.LogDebug("no keep-id specified, simply perform %v", cmd.Args)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}

	return nil
}

// DiscUsageMegaBytes returns disk usage for input path in MB (rounded).
func DiscUsageMegaBytes(path string) (string, error) {
	var discUsage int64

	readSize := func(path string, file os.FileInfo, err error) error {
		if !file.IsDir() {
			discUsage += file.Size()
		}

		return nil
	}

	err := filepath.Walk(path, readSize)
	if err != nil {
		logging.LogError("%v", err)

		return "", err
	}

	size := math.Round(float64(discUsage)/1024/1024) + 32

	return fmt.Sprintf("%.0fM", size), nil
}
