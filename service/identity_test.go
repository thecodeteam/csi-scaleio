package service_test

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"

	"github.com/thecodeteam/csi-scaleio/core"
	"github.com/thecodeteam/csi-scaleio/service"
	"github.com/thecodeteam/gocsi"
)

func TestPluginInfo(t *testing.T) {

	ctx := context.Background()

	gclient, stop := startServer(ctx, t)
	defer stop()

	client := csi.NewIdentityClient(gclient)

	info, err := client.GetPluginInfo(ctx,
		&csi.GetPluginInfoRequest{
			Version: &gocsi.ParseVersions(service.SupportedVersions)[0],
		})

	assert.NoError(t, err)
	assert.Equal(t, info.GetName(), service.Name)
	assert.Equal(t, info.GetVendorVersion(), core.SemVer)
}

func TestGetSupportedVersions(t *testing.T) {

	ctx := context.Background()

	gclient, stop := startServer(ctx, t)
	defer stop()

	client := csi.NewIdentityClient(gclient)

	vers, err := client.GetSupportedVersions(ctx,
		&csi.GetSupportedVersionsRequest{})

	assert.NoError(t, err)
	assert.NotEmpty(t, vers.GetSupportedVersions())
}
