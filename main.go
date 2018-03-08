//go:generate go generate ./core

package main

import (
	"context"

	"github.com/rexray/gocsi"

	"github.com/thecodeteam/csi-scaleio/provider"
	"github.com/thecodeteam/csi-scaleio/service"
)

// main is ignored when this package is built as a go plug-in
func main() {
	gocsi.Run(
		context.Background(),
		service.Name,
		"A ScaleIO Container Storage Interface (CSI) Plugin",
		usage,
		provider.New())
}

const usage = `    X_CSI_SCALEIO_ENDPOINT
        Specifies the HTTP endpoint for the ScaleIO gateway. This parameter is
        required when running the Controller service.

        The default value is empty.

    X_CSI_SCALEIO_USER
        Specifies the user name when authenticating to the ScaleIO Gateway.

        The default value is admin.

    X_CSI_SCALEIO_PASSWORD
        Specifies the password of the user defined by X_CSI_SCALEIO_USER to use
        when authenticating to the ScaleIO Gateway. This parameter is required
        when running the Controller service.

        The default value is empty.

    X_CSI_SCALEIO_INSECURE
        Specifies that the ScaleIO Gateway's hostname and certificate chain
	should not be verified.

        The default value is false.

    X_CSI_SCALEIO_SYSTEMNAME
        Specifies the name of the ScaleIO system to interact with.

        The default value is default.

    X_CSI_SCALEIO_SDCGUID
        Specifies the GUID of the SDC. This is only used by the Node Service,
        and removes a need for calling an external binary to retrieve the GUID.
        If not set, the external binary will be invoked.

        The default value is empty.

    X_CSI_SCALEIO_THICKPROVISIONING
        Specifies whether thick provisiong should be used when creating volumes.

        The default value is false.
`
