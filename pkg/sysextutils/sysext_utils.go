// Package sysextutils contains helpers and utilities for managing and creating
// sysexts.
package sysextutils

import (
	"bytes"
	"crypto/md5"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/89luca89/oci-sysext/pkg/fileutils"
	"github.com/89luca89/oci-sysext/pkg/imageutils"
	"github.com/89luca89/oci-sysext/pkg/logging"
	"github.com/89luca89/oci-sysext/pkg/utils"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// SysextDir is the default location for downloaded images.
var (
	SysextDir       = filepath.Join(utils.GetOciSysextHome(), "sysexts")
	SysextRootfsDir = filepath.Join(utils.GetOciSysextHome(), "sysexts-rootfs")
)

// GetID returns the md5sum based ID for given name.
func getID(name string) string {
	hasher := md5.New()

	_, err := io.WriteString(hasher, name)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))
}

// May be naive. Forgive me :)

func adjustSymlinks(rootfsDir string) error {
	return filepath.Walk(rootfsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				logging.LogError("Failed to read symlink: %s, error: %v", path, err)
				return err
			}
			if filepath.IsAbs(target) {
				newTarget := filepath.Join(rootfsDir, strings.TrimPrefix(target, "/"))
				if err := os.Remove(path); err != nil {
					logging.LogError("Failed to remove old symlink: %s, error: %v", path, err)
					return err
				}
				if err := os.Symlink(newTarget, path); err != nil {
					logging.LogError("Failed to create new symlink: %s -> %s, error: %v", path, newTarget, err)
					return err
				}
				logging.Log("Updated symlink: %s -> %s", path, newTarget)
			} else {
				relativeTarget := filepath.Join(filepath.Dir(path), target)
				if _, err := os.Stat(relativeTarget); os.IsNotExist(err) {
					newTarget := filepath.Join(rootfsDir, target)
					if err := os.Remove(path); err != nil {
						logging.LogError("Failed to remove old symlink: %s, error: %v", path, err)
						return err
					}
					if err := os.Symlink(newTarget, path); err != nil {
						logging.LogError("Failed to create new symlink: %s -> %s, error: %v", path, newTarget, err)
						return err
					}
					logging.Log("Updated relative symlink: %s -> %s", path, newTarget)
				}
			}
		}
		return nil
	})
}

// isStaticallyLinked determines if the specified binary is statically linked.
func isStaticallyLinked(path string) bool {
	f, err := elf.Open(path)
	if err != nil {
		logging.LogError("Failed to open file %s: %v", path, err)
		return false // Assume not statically linked if unable to open
	}
	defer f.Close()

	// Check if the INTERP section is present
	section := f.Section(".interp")
	if section == nil {
		// No .interp section means it's likely a statically linked binary
		return true
	}

	// Attempt to read data from the section to further verify
	data, err := section.Data()
	if err != nil {
		logging.LogError("Failed to read .interp section data: %v", err)
		return true // Assume statically linked if data cannot be read
	}

	// If the .interp section is empty, it's also considered statically linked
	if len(data) == 0 {
		return true
	}

	return false // .interp section is present and has data
}

func relocateAndPatchBinaries(rootfsDir, newRootPath string) error {
	logging.Log("Starting to relocate and patch binaries in %s", rootfsDir)

	err := filepath.Walk(rootfsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logging.LogError("Error accessing path %s: %v", path, err)
			return err
		}

		if !info.IsDir() && isELFExecutable(path) {
			if isStaticallyLinked(path) {
				logging.Log("Skipping patching for statically linked binary: %s", path)
				return nil
			}

			// Existing code for patching
			logging.Log("Patching binary: %s", path)
			if err := patchBinary(path, newRootPath); err != nil {
				logging.LogError("Failed to patch binary %s: %v", path, err)
				return err
			}

			if err := patchDependencies(path, newRootPath); err != nil {
				logging.LogError("Failed to patch dependencies for %s: %v", path, err)
				return err
			}
		}
		return nil
	})

	if err != nil {
		logging.LogError("Failed during the walking process in %s: %v", rootfsDir, err)
		return err
	}

	logging.Log("Successfully relocated and patched all binaries in %s", rootfsDir)
	return nil
}

// isELFExecutable checks if the file is an ELF executable
func isELFExecutable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	elfFile, err := elf.NewFile(f)
	if err != nil {
		return false
	}

	return elfFile.Type == elf.ET_EXEC || elfFile.Type == elf.ET_DYN
}

// patchBinary patches the binary's interpreter and library paths
func patchBinary(path, newRootPath string) error {
	// Check if the binary is statically linked; if so, skip patching
	if isStaticallyLinked(path) {
		logging.Log("Skipping interpreter patching for statically linked binary: %s", path)
		return nil
	}

	// Set the new interpreter path
	// newInterpreterPath := filepath.Join(newRootPath, "lib64", "ld-linux-x86-64.so.2") // doesn't work
	newInterpreterPath := "/lib64/ld-linux-x86-64.so.2" // works
	cmd := exec.Command("patchelf", "--set-interpreter", newInterpreterPath, path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	logCommand(cmd) // Log the command to file
	if err := cmd.Run(); err != nil {
		logging.LogError("Failed to patch interpreter for binary %s: %s", path, stderr.String())
		return err
	}

	// Set RPATH to ensure the binary finds the necessary libraries at the new location
	newRPath := filepath.Join(newRootPath, "lib")
	cmd = exec.Command("patchelf", "--set-rpath", newRPath, path)
	cmd.Stderr = &stderr
	logCommand(cmd) // Log the command to file
	if err := cmd.Run(); err != nil {
		logging.LogError("Failed to set RPATH for binary %s: %s", path, stderr.String())
		return err
	}

	logging.Log("Successfully patched binary %s", path)
	return nil
}

// patchDependencies updates the RPATH to include additional library paths and copies dependencies
func patchDependencies(path, newRootPath string) error {
	// Use ldd to find dependencies
	cmd := exec.Command("ldd", path)
	output, err := cmd.Output()
	if err != nil {
		logging.LogError("Failed to run ldd on %s: %v", path, err)
		return err
	}

	// Parse output to find needed libraries
	libraries := parseLddOutput(string(output))
	newLibPath := filepath.Join(newRootPath, "lib")

	for _, lib := range libraries {
		libName := filepath.Base(lib)
		newLibFullPath := filepath.Join(newLibPath, libName)
		logging.Log("Checking library: %s at path: %s", libName, newLibFullPath)

		if _, err := os.Stat(newLibFullPath); os.IsNotExist(err) {
			logging.Log("Library not found, copying: %s to %s", lib, newLibFullPath)
			if err := copyFile(lib, newLibFullPath); err != nil {
				logging.LogError("Failed to copy library %s to %s: %v", lib, newLibFullPath, err)
				return err
			}
		}

		// Update RPATH of the copied library
		cmd = exec.Command("patchelf", "--set-rpath", newLibPath, newLibFullPath)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		logCommand(cmd) // Log the command to file
		if err := cmd.Run(); err != nil {
			logging.LogError("Failed to set RPATH for library %s: %s", newLibFullPath, stderr.String())
			return err
		}
	}

	return nil
}

// logCommand logs the command to a file
func logCommand(cmd *exec.Cmd) {
	f, err := os.OpenFile("logs.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logging.LogError("Failed to open log file: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(cmd.String() + "\n"); err != nil {
		logging.LogError("Failed to write to log file: %v", err)
	}
}

// parseLddOutput parses the output from ldd to extract library names
func parseLddOutput(output string) []string {
	var libraries []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) > 2 && strings.Contains(parts[1], "=>") && parts[2] != "not" {
			libraries = append(libraries, parts[2])
		}
	}
	return libraries
}

// copyFile copies a file from src to dst and preserves file permissions
func copyFile(src, dst string) error {
	// Read the source file
	input, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}

	// Get the file mode (permissions) of the source file
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Write the file to the destination with the same permissions
	err = ioutil.WriteFile(dst, input, srcInfo.Mode())
	if err != nil {
		return err
	}

	return nil
}

func cleanRootfs(image, name string) error {
	sysextRootfsDIR := filepath.Join(SysextRootfsDir, getID(image))
	return os.RemoveAll(sysextRootfsDIR)
}

func calcSkipLayers(image, imageSource string) (int, error) {
	if image == imageSource || imageSource == "" {
		// No layers to skip if there is no differential source
		return 0, nil
	}

	logging.Log("reading %s's manifest", image)
	imageDir := imageutils.GetPath(image)
	manifestFile, err := fileutils.ReadFile(filepath.Join(imageDir, "manifest.json"))
	if err != nil {
		return 0, err
	}

	sourceImageDir := imageutils.GetPath(imageSource)
	logging.Log("reading %s's manifest", imageSource)
	sourceManifestFile, err := fileutils.ReadFile(filepath.Join(sourceImageDir, "manifest.json"))
	if err != nil {
		return 0, err
	}

	var manifest v1.Manifest
	var sourceManifest v1.Manifest

	logging.Log("parsing %s's manifest", image)
	err = json.Unmarshal(manifestFile, &manifest)
	if err != nil {
		return 0, err
	}

	logging.Log("parsing %s's manifest", imageSource)
	err = json.Unmarshal(sourceManifestFile, &sourceManifest)
	if err != nil {
		return 0, err
	}

	return len(manifest.Layers) - len(sourceManifest.Layers), nil
}

// createRootfs will generate a chrootable rootfs from input oci image reference, with input name and config.
// If input image is not found it will be automatically pulled.
// This function will read the oci-image manifest and properly unpack the layers in the right order to generate
// a valid rootfs.
// Untarring process will follow the keep-id option if specified in order to ensure no permission problems.
func createRootfs(image string, name string, imageSource string) error {
	logging.Log("Starting rootfs creation for image: %s", image)

	skip, err := calcSkipLayers(image, imageSource)
	if err != nil {
		logging.LogError("Failed to calculate skip layers: %v", err)
		return err
	}
	logging.Log("Calculated skip layers: %d", skip)

	sysextRootfsDIR := filepath.Join(SysextRootfsDir, getID(image))
	logging.Log("Creating directory: %s", sysextRootfsDIR)

	err = os.MkdirAll(sysextRootfsDIR, os.ModePerm)
	if err != nil {
		logging.LogError("Failed to create directory: %v", err)
		return err
	}

	logging.Log("Directory created successfully: %s", sysextRootfsDIR)

	logging.Log("Looking up image %s", image)
	imageDir := imageutils.GetPath(image)
	logging.Log("Reading manifest from %s", imageDir)

	manifestFile, err := fileutils.ReadFile(filepath.Join(imageDir, "manifest.json"))
	if err != nil {
		logging.LogError("Failed to read manifest file: %v", err)
		return err
	}

	var manifest v1.Manifest
	err = json.Unmarshal(manifestFile, &manifest)
	if err != nil {
		logging.LogError("Failed to unmarshal manifest file: %v", err)
		return err
	}

	logging.Log("Manifest unmarshalled successfully, extracting layers...")

	if skip < 0 || skip > len(manifest.Layers) {
		logging.LogError("Invalid number of layers to skip: %d", skip)
		return errors.New("invalid number of layers to skip")
	}

	for i, layer := range manifest.Layers {
		if i < skip {
			logging.Log("Skipping layer %s", layer.Digest)
			continue
		}

		layerDigest := strings.Split(layer.Digest.String(), ":")[1] + ".tar.gz"
		logging.Log("Extracting layer %s in %s", layerDigest, sysextRootfsDIR)

		err = fileutils.UntarFile(filepath.Join(imageDir, layerDigest), sysextRootfsDIR)
		if err != nil {
			logging.LogError("Failed to extract layer %s: %v", layerDigest, err)
			return err
		}
	}

	logging.Log("All layers extracted successfully")

	// Adjust symlinks before patching binaries
	logging.Log("Adjusting symlinks in %s", sysextRootfsDIR)
	err = adjustSymlinks(sysextRootfsDIR)
	if err != nil {
		logging.LogError("Failed to adjust symlinks: %v", err)
		return err
	}

	logging.Log("Symlinks adjusted successfully")

	// Assuming newRootPath is the same as sysextRootfsDIR for this example
	newRootPath := sysextRootfsDIR
	err = relocateAndPatchBinaries(sysextRootfsDIR, newRootPath)
	if err != nil {
		logging.LogError("Failed to relocate and patch binaries: %v", err)
		return err
	}

	logging.Log("Binaries relocated and patched successfully")

	dirs, err := os.ReadDir(sysextRootfsDIR)
	if err != nil {
		logging.LogError("Failed to read directory: %v", err)
		return err
	}

	for _, dir := range dirs {
		if dir.Name() != "usr" && dir.Name() != "opt" {
			logging.Log("Removing unneeded dir: %s", dir.Name())
			// os.RemoveAll(filepath.Join(sysextRootfsDIR, dir.Name()))
		}
	}

	err = os.MkdirAll(filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/"), os.ModePerm)
	if err != nil {
		logging.LogError("Failed to create extension-release directory: %v", err)
		return err
	}

	filePath := filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/", "extension-release."+name)
	content := "ID=_any\nEXTENSION_RELOAD_MANAGER=1\n"

	// Write the string to the file
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		logging.LogError("Failed to write extension-release file: %v", err)
		return err
	}

	logging.Log("Rootfs creation completed successfully")
	return nil
}

func CreateSysext(image string, name string, fs string, imageSource string) error {
	if fs != "squashfs" && fs != "btrfs" && fs != "ext4" {
		return errors.New("unsupported fs type")
	}

	// If imageSource is empty, use the full image and skip differential processing
	if imageSource == "" {
		imageSource = image // Optional: Set imageSource to image if you want to use the same image for some operations
	}

	// Ensure the image source directory only if imageSource is not the same as image
	if imageSource != image {
		sourceImageDir := imageutils.GetPath(imageSource)
		if !fileutils.Exist(sourceImageDir) {
			_, err := imageutils.Pull(imageSource, false)
			if err != nil {
				return err
			}
		}
	}

	logging.Log("cleaning up rootfs dir...")
	err := cleanRootfs(image, name)
	if err != nil {
		return err
	}

	logging.Log("ensuring image %s ...", imageSource)
	sourceImageDir := imageutils.GetPath(imageSource)
	if !fileutils.Exist(sourceImageDir) {
		_, err := imageutils.Pull(imageSource, false)
		if err != nil {
			return err
		}
	}

	err = createRootfs(image, name, imageSource)
	if err != nil {
		return err
	}

	err = os.MkdirAll(SysextDir, os.ModePerm)
	if err != nil {
		return err
	}

	_ = os.Remove(filepath.Join(SysextDir, name+".raw"))

	sysextRootfsDIR := filepath.Join(SysextRootfsDir, getID(image))
	logging.Log("creating raw file")
	cmd := exec.Command("", "")

	if fs == "squashfs" {
		cmd = exec.Command("mksquashfs", []string{
			sysextRootfsDIR,
			filepath.Join(SysextDir, name+".raw"),
		}...)
	} else if fs == "btrfs" {
		cmd = exec.Command("mkfs.btrfs", []string{
			"--mixed",
			"-m",
			"single",
			"-d",
			"single",
			"--shrink",
			"--rootdir",
			sysextRootfsDIR,
			filepath.Join(SysextDir, name+".raw"),
		}...)
	} else if fs == "ext4" {
		size, err := fileutils.DiscUsageMegaBytes(sysextRootfsDIR)
		if err != nil {
			return err
		}

		logging.Log("creating image of size %s", size)
		out, err := exec.Command("truncate", []string{
			"-s", size, filepath.Join(SysextDir, name+".raw"),
		}...).CombinedOutput()
		if err != nil {
			logging.LogError(string(out))
			return err
		}

		logging.Log("mkfs.ext4")
		out, err = exec.Command("mkfs.ext4", []string{
			"-E",
			"root_owner=0:0",
			"-d",
			sysextRootfsDIR,
			filepath.Join(SysextDir, name+".raw"),
		}...).CombinedOutput()
		if err != nil {
			logging.LogError(string(out))
			return err
		}

		logging.Log("resize2fs")
		out, err = exec.Command("resize2fs", []string{"-M", filepath.Join(SysextDir, name+".raw")}...).CombinedOutput()
		if err != nil {
			logging.LogError(string(out))
			return err
		}

		return nil
	} else {
		return errors.New("Unsupported fs type")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.LogError(string(output))
	}
	return err
}
