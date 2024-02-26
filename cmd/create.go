// Package cmd contains all the cobra commands for the CLI application.
package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/89luca89/oci-sysext/pkg/logging"
	"github.com/89luca89/oci-sysext/pkg/sysextutils"
	"github.com/spf13/cobra"
)

// NewCreateCommand will create a new container environment ready to use.
func NewCreateCommand() *cobra.Command {
	createCommand := &cobra.Command{
		Use:              "create [flags] IMAGE [COMMAND] [ARG...]",
		Short:            "Create but do not start a container",
		PreRunE:          logging.Init,
		RunE:             create,
		SilenceUsage:     true,
		SilenceErrors:    true,
		TraverseChildren: true,
	}

	createCommand.Flags().SetInterspersed(false)
	createCommand.Flags().Bool("help", false, "show help")
	createCommand.Flags().String("image", "", "OCI image to use")
	createCommand.Flags().String("name", "", "name of sysext")
	createCommand.Flags().String("fs", "ext4", "fs to use for raw image")
	createCommand.Flags().String("image-source", "", "source image to diff-out of the specified image")
	return createCommand
}

func create(cmd *cobra.Command, arguments []string) error {
	image, err := cmd.Flags().GetString("image")
	if err != nil {
		return err
	}

	name, err := cmd.Flags().GetString("name")
	if err != nil {
		return err
	}

	fs, err := cmd.Flags().GetString("fs")
	if err != nil {
		return err
	}

	imageSource, err := cmd.Flags().GetString("image-source")
	if err != nil {
		return err
	}

	if image == "" || name == "" || imageSource == "" {
		out, _ := exec.Command("/proc/self/exe", []string{"create", "--help"}...).CombinedOutput()
		fmt.Println(string(out))
		return errors.New("Missing arguments")
	}

	return sysextutils.CreateSysext(image, name, fs, imageSource)
}
