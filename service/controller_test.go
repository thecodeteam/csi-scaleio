package service_test

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"

	"github.com/thecodeteam/csi-scaleio/service"
)

func TestControllerGetCaps(t *testing.T) {

	ctx := context.Background()

	gclient, stop := startServer(ctx, t)
	defer stop()

	client := csi.NewControllerClient(gclient)

	rpcs := map[csi.ControllerServiceCapability_RPC_Type]struct{}{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME:     struct{}{},
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME: struct{}{},
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES:             struct{}{},
		csi.ControllerServiceCapability_RPC_GET_CAPACITY:             struct{}{},
	}

	resp, err := client.ControllerGetCapabilities(ctx,
		&csi.ControllerGetCapabilitiesRequest{
			Version: service.SupportedVersions[0],
		})

	assert.NoError(t, err)
	caps := resp.GetCapabilities()
	assert.NotEmpty(t, caps)
	assert.Len(t, caps, len(rpcs))

	for _, cap := range caps {
		assert.Contains(t, rpcs, cap.GetRpc().GetType())
		delete(rpcs, cap.GetRpc().GetType())
	}
	assert.Empty(t, rpcs)
}

/*
func TestControllerProbe(t *testing.T) {
	ctx := context.Background()

	os.Setenv("X_CSI_SCALEIO_ENDPOINT", "https://localhost:443")

	gclient, stop := startServer(ctx, t)
	defer stop()

	client := csi.NewControllerClient(gclient)
}
*/
