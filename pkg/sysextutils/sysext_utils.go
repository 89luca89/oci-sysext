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

// CreateRootfs will generate a chrootable rootfs from input oci image reference, with input name and config.
// If input image is not found it will be automatically pulled.
// This function will read the oci-image manifest and properly unpack the layers in the right order to generate
// a valid rootfs.
// Untarring process will follow the keep-id option if specified in order to ensure no permission problems.
func CreateRootfs(image string, name string, osname string) error {
	logging.Log("preparing rootfs for new sysext %s", name)

	sysextRootfsDIR := filepath.Join(SysextRootfsDir, getID(image))

	logging.Log("creating %s", sysextRootfsDIR)

	err := os.MkdirAll(sysextRootfsDIR, os.ModePerm)
	if err != nil {
		return err
	}

	logging.Log("looking up image %s", image)

	imageDir := imageutils.GetPath(image)
	if !fileutils.Exist(imageDir) {
		_, err := imageutils.Pull(image, false)
		if err != nil {
			return err
		}
	}

	logging.Log("reading %s's manifest", image)

	// get manifest
	manifestFile, err := fileutils.ReadFile(filepath.Join(imageDir, "manifest.json"))
	if err != nil {
		return err
	}

	var manifest v1.Manifest

	err = json.Unmarshal(manifestFile, &manifest)
	if err != nil {
		return err
	}

	logging.Log("extracting image's layers")

	for _, layer := range manifest.Layers {
		layerDigest := strings.Split(layer.Digest.String(), ":")[1] + ".tar.gz"

		logging.Log("extracting layer %s in %s", layerDigest, sysextRootfsDIR)

		err = fileutils.UntarFile(
			filepath.Join(imageDir, layerDigest),
			sysextRootfsDIR,
		)
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

			os.RemoveAll(filepath.Join(sysextRootfsDIR, dir.Name()))
		}
	}

	err = os.MkdirAll(filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/"), os.ModePerm)
	if err != nil {
		return err
	}

	filePath := filepath.Join(sysextRootfsDIR, "/usr/lib/extension-release.d/", "extension-release."+name)

	content := "ID=" + osname + "\nARCHITECTURE=x86-64"

	// Write the string to the file
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return err
	}

	logging.Log("extracting done")

	return nil
}

func CreateSysext(image string, name string, fs string, osname string) error {
	if fs != "squashfs" && fs != "btrfs" {
		return errors.New("Unsupported fs type")
	}

	err := CreateRootfs(image, name, osname)
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
	} else {
		return errors.New("Unsupported fs type")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.LogError(string(output))
	}
	return err
}
