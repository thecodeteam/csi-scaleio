# CSI plugin for ScaleIO [![Build Status](http://travis-ci.org/thecodeteam/csi-scaleio.svg?branch=master)]

## Description
CSI-ScaleIO is a Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec))
plugin that provides ScaleIO support.

This project may be compiled as a stand-alone binary using Golang that, when
run, provides a valid CSI endpoint. This project can also be vendored or built
as a Golang plug-in in order to extend the functionality of other programs.

## Runtime Dependencies
The Node portion of the plugin can be run on any node that is configured as a
ScaleIO SDC. This means that the `scini` kernel module must be loaded. Also,
if the `X_CSI_SCALEIO_SDCGUID` environment variable is not set, the plugin will
try to query the SDC GUID by executing the binary
`/opt/emc/scaleio/sdc/bin/drv_cfg`. If that binary is not present, the Node
Service cannot be run.

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

## Start plugin
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

## Using plugin
The CSI specification uses the gRPC protocol for plug-in communication.
The easiest way to interact with a CSI plugin is via the Container
Storage Client (`csc`) program provided via the
[GoCSI](https://github.com/thecodeteam/gocsi) project:

```bash
$ go get github.com/thecodeteam/gocsi
$ go install github.com/thecodeteam/gocsi/csc
```

Then, set have `csc` use the same `CSI_ENDPOINT`, and you can issue commands
to the plugin. Some examples...

Get the plugin's supported versions and plugin info:

```bash
$ ./csc -e csi.sock identity supported-versions
0.1.0

$ ./csc -v 0.1.0 -e csi.sock identity plugin-info
"com.thecodeteam.scaleio"	"0.0.1+1"
"commit"="cd9c538b596db926a3a747c6c219a2ace8f1890b"
"formed"="Fri, 01 Dec 2017 08:33:28 PST"
"semver"="0.0.1+1"
"url"="https://github.com/thecodeteam/csi-scaleio"
```

### Parameters
When using the plugin, some commands accept additional parameters, some of which
may be required for the command to work, or may change the behavior of the
command. Those parameters are listed here.

* `CreateVolume`: `storagepool` The name of a storage pool *must* be passed
  in the `CreateVolume` command
* `GetCapacity`: `storagepool` *may* be passed in `GetCapacity` command. If it
  is, the returned capacity is the available capacity for creation within the
  given storage pool. Otherwise, it's the capacity for creation within the
  storage cluster.

Passing parameters with `csc` is demonstrated in this `CreateVolume` command:

```bash
$ ./csc -v 0.1.0 c create --cap 1,mount,xfs --params storagepool=pd1pool1 myvol
"6757e7d300000000"
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
| `X_CSI_SCALEIO_SYSTEMNAME` | The name of the ScaleIO cluster | "" | `true` |
| `X_CSI_SCALEIO_SDCGUID` | The GUID of the SDC. This is only used by the Node Service, and removes a need for calling an external binary to retrieve the GUID | "" | `false` |
| `X_CSI_SCALEIO_THICKPROVISIONING` | Whether to use thick provisioning when creating new volumes | `false` | `false` |

## Capable operational modes
The CSI spec defines a set of AccessModes that a volume can have. CSI-ScaleIO
supports the following modes for volumes that will be mounted as a filesystem:

```
// Can only be published once as read/write on a single node,
// at any given time.
SINGLE_NODE_WRITER = 1;

// Can only be published once as readonly on a single node,
// at any given time.
SINGLE_NODE_READER_ONLY = 2;

// Can be published as readonly at multiple nodes simultaneously.
MULTI_NODE_READER_ONLY = 3;
```

This means that volumes can be mounted to either single node at a time, with
read-write or read-only permission, or can be mounted on multiple nodes, but all
must be read-only.

For volumes that are used as block devices, only the following are supported:

```
// Can only be published once as read/write on a single node, at
// any given time.
SINGLE_NODE_WRITER = 1;

// Can be published as read/write at multiple nodes
// simultaneously.
MULTI_NODE_MULTI_WRITER = 5;
```

This means that giving a workload read-only access to a block device is not
supported.

In general, volumes should be formatted with xfs or ext4.

## Support
For any questions or concerns please file an issue with the
[csi-scaleio](https://github.com/thecodeteam/csi-scaleio/issues) project or join
the Slack channel #project-rexray at codecommunity.slack.com.
