package service

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"github.com/thecodeteam/goscaleio"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	drvCfg = "/opt/emc/scaleio/sdc/bin/drv_cfg"
)

var (
	emptyNodePubResp   = &csi.NodePublishVolumeResponse{}
	emptyNodeUnpubResp = &csi.NodeUnpublishVolumeResponse{}
)

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {

	id := req.GetVolumeId()

	sdcMappedVol, err := getMappedVol(id)
	if err != nil {
		return nil, err
	}

	if err := publishVolume(req, s.privDir, sdcMappedVol.SdcDevice); err != nil {
		return nil, err
	}

	return emptyNodePubResp, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	id := req.GetVolumeId()

	sdcMappedVol, err := getMappedVol(id)
	if err != nil {
		return nil, err
	}

	if err := unpublishVolume(req, s.privDir, sdcMappedVol.SdcDevice); err != nil {
		return nil, err
	}

	return emptyNodeUnpubResp, nil
}

func getMappedVol(id string) (*goscaleio.SdcMappedVolume, error) {
	// get source path of volume/device
	localVols, err := goscaleio.GetLocalVolumeMap()
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"unable to get locally mapped ScaleIO volumes: %s",
			err.Error())
	}
	var sdcMappedVol *goscaleio.SdcMappedVolume
	for _, v := range localVols {
		if v.VolumeID == id {
			sdcMappedVol = v
			break
		}
	}
	if sdcMappedVol == nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume: %s not published to node", id)
	}
	return sdcMappedVol, nil
}

func (s *service) GetNodeID(
	ctx context.Context,
	req *csi.GetNodeIDRequest) (
	*csi.GetNodeIDResponse, error) {

	if s.opts.SdcGUID == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"Unable to get Node ID. Either it is not configured, "+
				"or Node Service has not been probed")
	}
	return &csi.GetNodeIDResponse{
		NodeId: s.opts.SdcGUID,
	}, nil
}

func (s *service) NodeProbe(
	ctx context.Context,
	req *csi.NodeProbeRequest) (
	*csi.NodeProbeResponse, error) {

	if s.opts.SdcGUID == "" {
		// try to get GUID using `drv_cfg` binary
		if _, err := os.Stat(drvCfg); os.IsNotExist(err) {
			return nil, status.Error(codes.FailedPrecondition,
				"unable to get SDC GUID via config or drv_cfg binary")
		}

		out, err := exec.Command(drvCfg, "--query_guid").CombinedOutput()
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"error getting SDC GUID: %s", err.Error())
		}

		s.opts.SdcGUID = strings.TrimSpace(string(out))
		log.WithField("guid", s.opts.SdcGUID).Info("set SDC GUID")
	}

	if !kmodLoaded() {
		return nil, status.Error(codes.FailedPrecondition,
			"scini kernel module not loaded")
	}

	// make sure privDir is pre-created
	if _, err := mkdir(s.privDir); err != nil {
		return nil, status.Errorf(codes.Internal,
			"plugin private dir: %s creation error: %s",
			s.privDir, err.Error())
	}

	return &csi.NodeProbeResponse{}, nil
}

func kmodLoaded() bool {
	out, err := exec.Command("lsmod").CombinedOutput()
	if err != nil {
		log.WithError(err).Error("error from lsmod")
		return false
	}

	r := bytes.NewReader(out)
	s := bufio.NewScanner(r)

	for s.Scan() {
		l := s.Text()
		words := strings.Split(l, " ")
		if words[0] == "scini" {
			return true
		}
	}

	return false
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{}, nil
}
