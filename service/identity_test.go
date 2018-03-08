package service_test

import (
	"context"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/stretchr/testify/assert"

	"github.com/thecodeteam/csi-scaleio/core"
	"github.com/thecodeteam/csi-scaleio/service"
)

func TestPluginInfo(t *testing.T) {

	ctx := context.Background()

	gclient, stop := startServer(ctx, t)
	defer stop()

	client := csi.NewIdentityClient(gclient)

	info, err := client.GetPluginInfo(ctx,
		&csi.GetPluginInfoRequest{})

	assert.NoError(t, err)
	assert.Equal(t, info.GetName(), service.Name)
	assert.Equal(t, info.GetVendorVersion(), core.SemVer)
}
