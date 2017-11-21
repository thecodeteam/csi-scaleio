package service

import (
	xctx "golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) GetVolumeID(
	ctx xctx.Context,
	name string) (string, error) {

	return "", nil
}

func (s *service) GetVolumeInfo(
	ctx xctx.Context,
	id, name string) (*csi.VolumeInfo, error) {

	return nil, nil
}

func (s *service) IsControllerPublished(
	ctx xctx.Context,
	id, nodeID string) (map[string]string, error) {

	return nil, nil
}

func (s *service) IsNodePublished(
	ctx xctx.Context,
	id string,
	pubInfo map[string]string,
	targetPath string) (bool, error) {

	return false, nil
}
