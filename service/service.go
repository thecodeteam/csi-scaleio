package service

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"github.com/thecodeteam/gocsi"
	"github.com/thecodeteam/gocsi/csp"
	sio "github.com/thecodeteam/goscaleio"
	siotypes "github.com/thecodeteam/goscaleio/types/v1"

	"github.com/thecodeteam/csi-scaleio/core"
)

const (
	// Name is the name of the CSI plug-in.
	Name = "com.thecodeteam.scaleio"

	// SupportedVersions is a list of supported CSI versions.
	SupportedVersions = "0.1.0"

	// KeyThickProvisioning is the key used to get a flag indicating that
	// a volume should be thick provisioned from the volume create params
	KeyThickProvisioning = "thickprovisioning"

	thinProvisioned  = "ThinProvisioned"
	thickProvisioned = "ThickProvisioned"
	defaultPrivDir   = "/dev/disk/csi-scaleio"
)

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"url":    "https://github.com/thecodeteam/csi-scaleio",
	"semver": core.SemVer,
	"commit": core.CommitSha32,
	"formed": core.CommitTime.Format(time.RFC1123),
}

// Service is the CSI Mock service provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
	gocsi.IdempotencyProvider
	BeforeServe(context.Context, *csp.StoragePlugin, net.Listener) error
}

type Opts struct {
	Endpoint   string
	User       string
	Password   string
	SystemName string
	SdcGUID    string
	Insecure   bool
	Thick      bool
}

type service struct {
	opts        Opts
	adminClient *sio.Client
	system      *sio.System
	volCache    []*siotypes.Volume
	volCacheRWL sync.RWMutex
	sdcMap      map[string]string
	sdcMapRWL   sync.RWMutex
	spCache     map[string]string
	spCacheRWL  sync.RWMutex
	privDir     string
}

// New returns a new Service.
func New() Service {
	return &service{
		sdcMap:  map[string]string{},
		spCache: map[string]string{},
	}
}

func (s *service) BeforeServe(
	ctx context.Context, sp *csp.StoragePlugin, lis net.Listener) error {

	defer func() {
		fields := map[string]interface{}{
			"endpoint":       s.opts.Endpoint,
			"user":           s.opts.User,
			"password":       "",
			"systemname":     s.opts.SystemName,
			"sdcGUID":        s.opts.SdcGUID,
			"insecure":       s.opts.Insecure,
			"thickprovision": s.opts.Thick,
			"privatedir":     s.privDir,
		}

		if s.opts.Password != "" {
			fields["password"] = "******"
		}

		log.WithFields(fields).Infof("configured %s", Name)
	}()

	opts := Opts{}

	if ep, ok := gocsi.LookupEnv(ctx, EnvEndpoint); ok {
		opts.Endpoint = ep
	}
	if user, ok := gocsi.LookupEnv(ctx, EnvUser); ok {
		opts.User = user
	}
	if opts.User == "" {
		opts.User = "admin"
	}
	if pw, ok := gocsi.LookupEnv(ctx, EnvPassword); ok {
		opts.Password = pw
	}
	if name, ok := gocsi.LookupEnv(ctx, EnvSystemName); ok {
		opts.SystemName = name
	}
	if guid, ok := gocsi.LookupEnv(ctx, EnvSDCGUID); ok {
		opts.SdcGUID = guid
	}
	var privDir string
	if pd, ok := gocsi.LookupEnv(ctx, csp.EnvVarPrivateMountDir); ok {
		privDir = pd
	}
	if privDir == "" {
		privDir = defaultPrivDir
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := gocsi.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				log.WithField(n, v).Debug(
					"invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.Insecure = pb(EnvInsecure)
	opts.Thick = pb(EnvThick)

	s.opts = opts
	s.privDir = privDir

	return nil
}

// getVolProvisionType returns a string indicating thin or thick provisioning
// If the type is specified in the params map, that value is used, if not, defer
// to the service config
func (s *service) getVolProvisionType(params map[string]string) string {
	volType := thinProvisioned
	if s.opts.Thick {
		volType = thickProvisioned
	}

	if tp, ok := params[KeyThickProvisioning]; ok {
		tpb, err := strconv.ParseBool(tp)
		if err != nil {
			log.Warnf("invalid boolean received `%s`=(%v) in params",
				KeyThickProvisioning, tp)
		} else if tpb {
			volType = thickProvisioned
		} else {
			volType = thinProvisioned
		}
	}

	return volType
}

func (s *service) getVolByID(id string) (*siotypes.Volume, error) {

	// The `GetVolume` API returns a slice of volumes, but when only passing
	// in a volume ID, the response will be just the one volume
	vols, err := s.adminClient.GetVolume("", id, "", "", false)
	if err != nil {
		return nil, err
	}
	return vols[0], nil
}

func (s *service) getSDCID(sdcGUID string) (string, error) {
	sdcGUID = strings.ToUpper(sdcGUID)

	// check if ID is already in cache
	f := func() string {
		s.sdcMapRWL.RLock()
		defer s.sdcMapRWL.RUnlock()

		if id, ok := s.sdcMap[sdcGUID]; ok {
			return id
		}
		return ""
	}
	if id := f(); id != "" {
		return id, nil
	}

	// Need to translate sdcGUID to sdcID
	id, err := s.system.FindSdc("SdcGuid", sdcGUID)
	if err != nil {
		return "", fmt.Errorf("error finding SDC from GUID: %s, err: %s",
			sdcGUID, err.Error())
	}

	s.sdcMapRWL.Lock()
	defer s.sdcMapRWL.Unlock()

	s.sdcMap[sdcGUID] = id.Sdc.ID

	return id.Sdc.ID, nil
}

func (s *service) getStoragePoolID(name string) (string, error) {
	// check if ID is already in cache
	f := func() string {
		s.spCacheRWL.RLock()
		defer s.spCacheRWL.RUnlock()

		if id, ok := s.spCache[name]; ok {
			return id
		}
		return ""
	}
	if id := f(); id != "" {
		return id, nil
	}

	// Need to lookup ID from the gateway
	pool, err := s.adminClient.FindStoragePool("", name, "")
	if err != nil {
		return "", err
	}

	s.spCacheRWL.Lock()
	defer s.spCacheRWL.Unlock()
	s.spCache[name] = pool.ID

	return pool.ID, nil
}

func getCSIVolumeInfo(vol *siotypes.Volume) *csi.VolumeInfo {

	vi := &csi.VolumeInfo{
		Id:            vol.ID,
		CapacityBytes: uint64(vol.SizeInKb) * bytesInKiB,
	}

	return vi
}
