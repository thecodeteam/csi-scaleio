package service

import (
	"strings"

	"golang.org/x/net/context"

	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"

	"github.com/thecodeteam/csi-scaleio/core"
)

func (s *service) GetPluginInfo(
	ctx context.Context,
	req *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error) {

	return &csi.GetPluginInfoResponse{
		Name:          Name,
		VendorVersion: core.SemVer,
		Manifest:      Manifest,
	}, nil
}

func (s *service) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error) {

	var rep csi.GetPluginCapabilitiesResponse
	if !strings.EqualFold(s.mode, "node") {
		rep.Capabilities = []*csi.PluginCapability{
			&csi.PluginCapability{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		}
	}
	return &rep, nil
}

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {

	if !strings.EqualFold(s.mode, "node") {
		if err := s.controllerProbe(ctx); err != nil {
			return nil, err
		}
	}
	if !strings.EqualFold(s.mode, "controller") {
		if err := s.nodeProbe(ctx); err != nil {
			return nil, err
		}
	}

	return &csi.ProbeResponse{}, nil
}
