// Package sysextutils contains helpers and utilities for managing and creating
// sysexts.
package sysextutils

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	logging.Log("preparing rootfs for new sysext %s", name)

	skip, err := calcSkipLayers(image, imageSource)
	if err != nil {
		return err
	}

	sysextRootfsDIR := filepath.Join(SysextRootfsDir, getID(image))
	logging.Log("creating %s", sysextRootfsDIR)

	err = os.MkdirAll(sysextRootfsDIR, os.ModePerm)
	if err != nil {
		return err
	}

	logging.Log("looking up image %s", image)
	imageDir := imageutils.GetPath(image)
	logging.Log("reading %s's manifest", image)
	manifestFile, err := fileutils.ReadFile(filepath.Join(imageDir, "manifest.json"))
	if err != nil {
		return err
	}

	var manifest v1.Manifest
	err = json.Unmarshal(manifestFile, &manifest)
	if err != nil {
		return err
	}

	logging.Log("extracting image's layers, skipping %d layers...", skip)
	if skip < 0 || skip > len(manifest.Layers) {
		return errors.New("Invalid number of layers to skip")
	}

	for i, layer := range manifest.Layers {
		if i < skip {
			logging.Log("skipping layer %s", layer.Digest)
			continue
		}

		layerDigest := strings.Split(layer.Digest.String(), ":")[1] + ".tar.gz"
		logging.Log("extracting layer %s in %s", layerDigest, sysextRootfsDIR)

		err = fileutils.UntarFile(filepath.Join(imageDir, layerDigest), sysextRootfsDIR)
		if err != nil {
			return err
		}
	}

	dirs, err := os.ReadDir(sysextRootfsDIR)
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if dir.Name() != "usr" && dir.Name() != "opt" {
			logging.Log("removing unneeded dir: %s", dir.Name())
			// os.RemoveAll(filepath.Join(sysextRootfsDIR, dir.Name()))
		}
	}

	err = os.MkdirAll(filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/"), os.ModePerm)
	if err != nil {
		return err
	}

	filePath := filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/", "extension-release."+name)
	content := "ID=_any\nEXTENSION_RELOAD_MANAGER=1\n"

	// Write the string to the file
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return err
	}

	logging.Log("rootfs creation done")
	return nil
}

func CreateSysext(image string, name string, fs string, imageSource string) error {
	if fs != "squashfs" && fs != "btrfs" && fs != "ext4" {
		return errors.New("Unsupported fs type")
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
