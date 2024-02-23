// Package utils contains generic helpers, utilities and structs.
package utils

import (
	"os"
	"path/filepath"
)

// OciSysextBinPath is the bin path internally used by oci-sysext.
var OciSysextBinPath = filepath.Join(GetOciSysextHome(), "bin")

// GetOciSysextHome will return where the program will save data.
// This function will search the environment or:
//
// OCI-SYSEXT_HOME
// XDG_DATA_HOME
// HOME
//
// These variable are searched in this order.
func GetOciSysextHome() string {
	if os.Getenv("OCI-SYSEXT_HOME") != "" {
		return filepath.Join(os.Getenv("OCI-SYSEXT_HOME"), "oci-sysext")
	}

	if os.Getenv("XDG_DATA_HOME") != "" {
		return filepath.Join(os.Getenv("XDG_DATA_HOME"), "oci-sysext")
	}

	return filepath.Join(os.Getenv("HOME"), ".local/share/oci-sysext")
}
