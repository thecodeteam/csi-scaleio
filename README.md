# CSI-ScaleIO [![Build Status](http://travis-ci.org/thecodeteam/csi-scaleio.svg?branch=master)]

CSI-ScaleIO is a Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec))
plugin that provides ScaleIO support.

This project may be compiled as a stand-alone binary using Golang that, when
run, provides a valid CSI endpoint. This project can also be vendored or built
as a Golang plug-in in order to extend the functionality of other programs.

## Installation
CSI-ScaleIO can be installed with Go and the following command:

`$ go get github.com/thecodeteam/csi-scaleio`

The resulting binary will be installed to `$GOPATH/bin/csi-scaleio`.

If you want to build `csi-scaleio` with accurate version information, you'll
need to run the `go generate` command and build again:

```bash
$ go get github.com/thecodeteam/csi-scaleio
$ cd $GOPATH/src/github.com/thecodeteam/csi-scaleio
$ go generate && go install
```

The binary will once again be installed to `$GOPATH/bin/csi-scaleio`.

## Starting the Plug-in
Before starting the plugin please set the environment variable
`CSI_ENDPOINT` to a valid Go network address such as `csi.sock`:

```bash
$ CSI_ENDPOINT=csi.sock csi-scaleio
INFO[0000] configured com.thecodeteam.scaleio            endpoint="https://10.50.10.100:443" insecure=true password="******" privatedir=/dev/disk/csi-scaleio sdcGUID= systemname=democluster thickprovision=false user=admin
INFO[0000] identity service registered
INFO[0000] controller service registered
INFO[0000] node service registered
INFO[0000] serving                                       endpoint="unix:///csi.sock"
```

The server can be shutdown by using `Ctrl-C` or sending the process
any of the standard exit signals.

## Using the Plug-in
The CSI specification uses the gRPC protocol for plug-in communication.
The easiest way to interact with a CSI plug-in is via the Container
Storage Client (`csc`) program provided via the
[GoCSI](https://github.com/thecodeteam/gocsi) project:

```bash
$ go get github.com/thecodeteam/gocsi
$ go install github.com/thecodeteam/gocsi/csc
```

## Configuration
The CSI-ScaleIO SP is built using the GoCSI CSP package. Please
see its
[configuration section](https://github.com/thecodeteam/gocsi/tree/master/csp#configuration)
for a complete list of the environment variables that may be used to
configure this SP.

The following table is a list of this SP's default configuration values:

| Name | Value |
|------|---------|
| `X_CSI_IDEMP` | `true` |
| `X_CSI_IDEMP_REQUIRE_VOL` | `true` |
| `X_CSI_REQUIRE_NODE_ID` | `true` |
| `X_CSI_REQUIRE_PUB_VOL_INFO` | `false` |
| `X_CSI_CREATE_VOL_ALREADY_EXISTS` | `true` |
| `X_CSI_DELETE_VOL_NOT_FOUND` | `true` |
| `X_CSI_SUPPORTED_VERSIONS` | `0.1.0` |
| `X_CSI_PRIVATE_MOUNT_DIR` | `/dev/disk/csi-scaleio` |

The following table is a list of this configuration values that are specific
to ScaleIO, their default values, and whether they are required for operation:

| Name | Description | Default Val | Required |
|------|-------------|-------------|----------|
| `X_CSI_SCALEIO_ENDPOINT` | ScaleIO Gateway HTTP endpoint | "" | `true` |
| `X_CSI_SCALEIO_USER`     | Username for authenticating to Gateway | "admin" | `false` |
| `X_CSI_SCALEIO_PASSWORD` | Password of Gateway user | "" | `true` |
| `X_CSI_SCALEIO_INSECURE` | The ScaleIO Gateway's certificate chain and host name should not be verified | `false` | `false` |
| `X_CSI_SCALEIO_SYSTEMNAME` | The name of the ScaleIO cluster | "default" | `false` |
| `X_CSI_SCALEIO_SDCGUID` | The GUID of the SDC. This is only used by the Node Service, and removes a need for calling an external binary to retrieve the GUID | "" | `false` |
| `X_CSI_SCALEIO_THICKPROVISIONING` | Whether to use thick provisioning when creating new volumes | `false` | `false` |
