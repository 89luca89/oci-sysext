# OCI-SYSEXT


This tool aims to  be a little bridge to transform an OCI image into a systemd-sysext compatible raw image.

## Compile

`CGO_ENABLED=0 go build -mod vendor -ldflags="-s -w"`

## Usage

```sh
./oci-sysext pull docker.io/alpine:latest
 ./oci-sysext create --image docker.io/alpine:latest --name my-alpine-raw --fs btrfs --os opensuse-microos
```

### Usage notes

- Supported `--fs` are `squashfs` and `btrfs` 

- `--os` flag is the `/etc/os-release` content of `ID=`, this is because systemd-sysext needs to know what systemd this will run on
