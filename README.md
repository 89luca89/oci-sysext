# OCI-SYSEXT

This tool aims to  be a little bridge to transform an OCI image into a systemd-sysext compatible raw image.

## Compile

`CGO_ENABLED=0 go build -mod vendor -ldflags="-s -w"`

## Usage

```sh
./oci-sysext pull cgr.dev/chainguard/wolfi-base
 ./oci-sysext create --image cgr.dev/chainguard/wolfi-base --name wolfi-zero-cve-userspace --fs btrfs --image-source <optional-to-diff-against>
```

### Usage notes

- Supported `--fs` are `squashfs` and `btrfs` 
