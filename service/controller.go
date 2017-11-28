package service

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"github.com/thecodeteam/goscaleio"
	siotypes "github.com/thecodeteam/goscaleio/types/v1"

	"github.com/thecodeteam/gocsi"
)

const (
	// KeyStoragePool is the key used to get the storagepool name from the
	// volume create parameters map
	KeyStoragePool = "storagepool"

	// DefaultVolumeSizeKiB is default volume size to create on a scaleIO
	// cluster when no size is given, expressed in KiB
	DefaultVolumeSizeKiB = 16 * kiBytesInGiB

	// VolSizeMultipleGiB is the volume size that ScaleIO creates volumes as
	// a multiple of, meaning that all volume sizes are a multiple of this
	// number
	VolSizeMultipleGiB = 8

	// bytesInKiB is the number of bytes in a kibibyte
	bytesInKiB = 1024

	// kiBytesInGiB is the number of kibibytes in a gibibyte
	kiBytesInGiB = 1024 * 1024

	// bytesInGiB is the number of bytes in a gibibyte
	bytesInGiB = kiBytesInGiB * bytesInKiB

	removeModeOnlyMe         = "ONLY_ME"
	sioGatewayVolumeNotFound = "Could not find the volume"
	errNoMultiMap            = "volume not enabled for mapping to multiple hosts"
	errUnknownAccessMode     = "access mode cannot be UNKNOWN"
	errNoMultiNodeWriter     = "multi-node with writer(s) only supported for block access type"
)

var (
	emptyDelResp       = &csi.DeleteVolumeResponse{}
	emptyProbeResp     = &csi.ControllerProbeResponse{}
	emptyCtrlPubResp   = &csi.ControllerPublishVolumeResponse{}
	emptyCtrlUnpubResp = &csi.ControllerUnpublishVolumeResponse{}
)

func (s *service) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	cr := req.GetCapacityRange()
	sizeInKiB, err := validateVolSize(cr)
	if err != nil {
		return nil, err
	}

	params := req.GetParameters()

	// We require the storagePool name for creation
	sp, ok := params[KeyStoragePool]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"`%s` is a required parameter", KeyStoragePool)
	}

	volType := s.getVolProvisionType(params)

	name := req.GetName()
	if name == "" {
		return nil, gocsi.ErrVolumeNameRequired
	}

	fields := map[string]interface{}{
		"name":        name,
		"sizeInKiB":   sizeInKiB,
		"storagePool": sp,
		"volType":     volType,
	}

	log.WithFields(fields).Info("creating volume")

	volumeParam := &siotypes.VolumeParam{
		Name:           name,
		VolumeSizeInKb: strconv.Itoa(sizeInKiB),
		VolumeType:     volType,
	}
	createResp, err := s.adminClient.CreateVolume(volumeParam, sp)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error when creating volume: %s", err.Error())
	}

	vol, err := s.getVolByID(createResp.ID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"error retrieving volume details: %s", err.Error())
	}
	vi := getCSIVolumeInfo(vol)

	csiResp := &csi.CreateVolumeResponse{
		VolumeInfo: vi,
	}

	s.clearCache()

	return csiResp, nil
}

func (s *service) clearCache() {
	s.volCacheRWL.Lock()
	defer s.volCacheRWL.Unlock()
	s.volCache = make([]*siotypes.Volume, 0)
}

// validateVolSize uses the CapacityRange range params to determine what size
// volume to create, and returns an error if volume size would be greater than
// the given limit. Returned size is in KiB
func validateVolSize(cr *csi.CapacityRange) (int, error) {

	minSize := cr.GetRequiredBytes()
	maxSize := cr.GetLimitBytes()

	if minSize == 0 {
		minSize = DefaultVolumeSizeKiB
	} else {
		minSize = minSize / bytesInKiB
	}

	var (
		sizeGiB uint64
		sizeKiB uint64
		sizeB   uint64
	)
	// ScaleIO creates volumes in multiples of 8GiB, rounding up.
	// Determine what actual size of volume will be, and check that
	// we do not exceed maxSize
	sizeGiB = minSize / kiBytesInGiB
	mod := sizeGiB % VolSizeMultipleGiB
	if mod > 0 {
		sizeGiB = sizeGiB - mod + VolSizeMultipleGiB
	}
	sizeB = sizeGiB * bytesInGiB
	if maxSize != 0 {
		if sizeB > maxSize {
			return 0, status.Errorf(
				codes.OutOfRange,
				"volume size %d > limit_bytes: %d", sizeB, maxSize)
		}
	}

	sizeKiB = sizeGiB * kiBytesInGiB
	return int(sizeKiB), nil
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	id := req.GetVolumeId()

	vol, err := s.getVolByID(id)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			// Since not found is actualy a successful delete, we
			// need to return a valid response
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before deletion: %s",
			err.Error())
	}

	if len(vol.MappedSdcInfo) > 0 {
		// Volume is in use
		return nil, status.Errorf(codes.FailedPrecondition,
			"volume in use by %s", vol.MappedSdcInfo[0].SdcID)
	}

	tgtVol := goscaleio.NewVolume(s.adminClient)
	tgtVol.Volume = vol
	err = tgtVol.RemoveVolume(removeModeOnlyMe)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error removing volume: %s", err.Error())
	}

	s.clearCache()

	return emptyDelResp, nil
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, gocsi.ErrVolumeIDRequired
	}

	vol, err := s.getVolByID(volID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before controller publish: %s",
			err.Error())
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, gocsi.ErrNodeIDRequired
	}

	sdcID, err := s.getSDCID(nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}

	vc := req.GetVolumeCapability()
	if vc == nil {
		return nil, gocsi.ErrVolumeCapabilityRequired
	}

	am := vc.GetAccessMode()
	if am == nil {
		return nil, gocsi.ErrAccessModeRequired
	}

	if am.Mode == csi.VolumeCapability_AccessMode_UNKNOWN {
		return nil, status.Error(codes.InvalidArgument,
			errUnknownAccessMode)
	}
	// Check if volume is published to any node already
	if len(vol.MappedSdcInfo) > 0 {
		vcs := []*csi.VolumeCapability{req.GetVolumeCapability()}
		isBlock := accTypeIsBlock(vcs)

		for _, sdc := range vol.MappedSdcInfo {
			if sdc.SdcID == sdcID {
				// volume already mapped
				log.Debug("volume already mapped")
				return emptyCtrlPubResp, nil
			}
		}

		if !vol.MappingToAllSdcsEnabled {
			return nil, status.Error(codes.AlreadyExists,
				errNoMultiMap)
		}

		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			return nil, status.Errorf(codes.AlreadyExists,
				"volume already published to SDC id: %s", vol.MappedSdcInfo[0].SdcID)

		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return nil, status.Error(codes.InvalidArgument,
				errNoMultiNodeWriter)
		}

		if !isBlock {
			// multi-mapping Mount volumes is never allowed
			return nil, status.Error(codes.AlreadyExists,
				errUnknownAccessMode)
		}
	}

	mapVolumeSdcParam := &siotypes.MapVolumeSdcParam{
		SdcID: sdcID,
		AllowMultipleMappings: "false",
		AllSdcs:               "",
	}

	targetVolume := goscaleio.NewVolume(s.adminClient)
	targetVolume.Volume = &siotypes.Volume{ID: vol.ID}

	err = targetVolume.MapVolumeSdc(mapVolumeSdcParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error mapping volume to node: %s", err.Error())
	}

	return emptyCtrlPubResp, nil
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, gocsi.ErrVolumeIDRequired
	}

	vol, err := s.getVolByID(volID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before controller unpublish: %s",
			err.Error())
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, gocsi.ErrNodeIDRequired
	}

	sdcID, err := s.getSDCID(nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}

	// check if volume is attached to node at all
	mappedToNode := false
	for _, mapping := range vol.MappedSdcInfo {
		if mapping.SdcID == sdcID {
			mappedToNode = true
			break
		}
	}

	if !mappedToNode {
		return emptyCtrlUnpubResp, nil
	}

	targetVolume := goscaleio.NewVolume(s.adminClient)
	targetVolume.Volume = vol

	unmapVolumeSdcParam := &siotypes.UnmapVolumeSdcParam{
		SdcID:                sdcID,
		IgnoreScsiInitiators: "true",
		AllSdcs:              "",
	}

	if err = targetVolume.UnmapVolumeSdc(unmapVolumeSdcParam); err != nil {
		return nil, status.Errorf(codes.Internal,
			"error unmapping volume from node: %s", err.Error())
	}

	return emptyCtrlUnpubResp, nil
}

func (s *service) ValidateVolumeCapabilities(
	ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (
	*csi.ValidateVolumeCapabilitiesResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	vol, err := s.getVolByID(req.GetVolumeId())
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status for capabilities: %s",
			err.Error())
	}

	vcs := req.GetVolumeCapabilities()
	supported, reason := valVolumeCaps(vcs, vol)

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Supported: supported,
	}
	if !supported {
		resp.Message = reason
	}

	return resp, nil
}

func accTypeIsBlock(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if at := vc.GetBlock(); at != nil {
			return true
		}
	}
	return false
}

func valVolumeCaps(
	vcs []*csi.VolumeCapability,
	vol *siotypes.Volume) (bool, string) {

	var (
		supported = true
		isBlock   = accTypeIsBlock(vcs)
		reason    string
	)

	for _, vc := range vcs {
		am := vc.GetAccessMode()
		if am == nil {
			continue
		}
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_UNKNOWN:
			supported = false
			reason = errUnknownAccessMode
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			break
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			if !vol.MappingToAllSdcsEnabled {
				supported = false
				reason = errNoMultiMap
			}
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			if !vol.MappingToAllSdcsEnabled {
				supported = false
				reason = errNoMultiMap
			}
			if !isBlock {
				supported = false
				reason = errNoMultiNodeWriter
			}
		}
	}

	return supported, reason
}

func (s *service) ListVolumes(
	ctx context.Context,
	req *csi.ListVolumesRequest) (
	*csi.ListVolumesResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}

	var (
		startToken uint32
		cacheLen   uint32
	)

	if v := req.StartingToken; v != "" {
		i, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"unable to parse startingToken:%v into uint32",
				req.StartingToken)
		}
		startToken = uint32(i)
	}

	// Get the length of cached volumes. Do it in a funcion so as not to
	// hold the lock
	func() {
		s.volCacheRWL.RLock()
		defer s.volCacheRWL.RUnlock()
		cacheLen = uint32(len(s.volCache))
	}()

	var (
		lvols      uint32
		sioVols    []*siotypes.Volume
		err        error
		maxEntries = req.MaxEntries
	)

	if startToken == 0 || (startToken > 0 && cacheLen == 0) {
		// make call to cluster to get all volumes
		sioVols, err = s.adminClient.GetVolume("", "", "", "", false)
		if err != nil {
			return nil, status.Errorf(
				codes.Internal,
				"unable to list volumes: %s", err.Error())
		}

		lvols = uint32(len(sioVols))
		if maxEntries > 0 && maxEntries < lvols {
			// We want to cache this volume list so that we don't
			// have to get all the volumes again on the next call
			func() {
				s.volCacheRWL.Lock()
				defer s.volCacheRWL.Unlock()
				s.volCache = make([]*siotypes.Volume, lvols)
				copy(s.volCache, sioVols)
				cacheLen = lvols
			}()
		}
	} else {
		lvols = cacheLen
	}

	if startToken > lvols {
		return nil, status.Errorf(
			codes.Aborted,
			"startingToken=%d > len(vols)=%d",
			startToken, lvols)
	}

	// Discern the number of remaining entries.
	rem := lvols - startToken

	// If maxEntries is 0 or greater than the number of remaining entries then
	// set maxEntries to the number of remaining entries.
	if maxEntries == 0 || maxEntries > rem {
		maxEntries = rem
	}

	var (
		entries = make(
			[]*csi.ListVolumesResponse_Entry,
			maxEntries)
		source []*siotypes.Volume
	)

	if startToken == 0 && req.MaxEntries == 0 {
		// Use the just populated sioVols
		source = sioVols
	} else {
		// Return only the requested vols from the cache
		cacheVols := make([]*siotypes.Volume, maxEntries)
		// Copy vols from cache so we don't keep lock entire time
		func() {
			s.volCacheRWL.RLock()
			defer s.volCacheRWL.RUnlock()
			j := startToken
			for i := 0; i < len(entries); i++ {
				cacheVols[i] = s.volCache[i]
				j++
			}
		}()
		source = cacheVols
	}

	for i, vol := range source {
		entries[i] = &csi.ListVolumesResponse_Entry{
			VolumeInfo: getCSIVolumeInfo(vol),
		}
	}

	var nextToken string
	if n := startToken + uint32(len(source)); n < lvols {
		nextToken = fmt.Sprintf("%d", n)
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {

	if s.adminClient == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"Controller Service has not been probed")
	}
	/*
		return &csi.GetCapacityResponse{
			AvailableCapacity: tib100,
		}, nil
	*/
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *service) ControllerGetCapabilities(
	ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
					},
				},
			},
			/*
				&csi.ControllerServiceCapability{
					Type: &csi.ControllerServiceCapability_Rpc{
						Rpc: &csi.ControllerServiceCapability_RPC{
							Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
						},
					},
				},*/
		},
	}, nil
}

func (s *service) ControllerProbe(
	ctx context.Context,
	req *csi.ControllerProbeRequest) (
	*csi.ControllerProbeResponse, error) {

	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"missing ScaleIO Gateway endpoint")
	}
	if s.opts.User == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"missing ScaleIO MDM user")
	}
	if s.opts.Password == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"missing ScaleIO MDM password")
	}
	if s.opts.SystemName == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"missing ScaleIO system name")
	}

	// Create our ScaleIO API client, if needed
	if s.adminClient == nil {
		c, err := goscaleio.NewClientWithArgs(
			s.opts.Endpoint, "", s.opts.Insecure, true)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"unable to create ScaleIO client: %s", err.Error())
		}
		s.adminClient = c
	}

	if s.adminClient.GetToken() == "" {
		_, err := s.adminClient.Authenticate(&goscaleio.ConfigConnect{
			Endpoint: s.opts.Endpoint,
			Username: s.opts.User,
			Password: s.opts.Password,
		})
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"unable to login to ScaleIO Gateway: %s", err.Error())

		}
	}

	if s.system == nil {
		system, err := s.adminClient.FindSystem(
			"", s.opts.SystemName, "")
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"unable to find matching ScaleIO system name: %s",
				err.Error())
		}
		s.system = system
	}

	return emptyProbeResp, nil
}
