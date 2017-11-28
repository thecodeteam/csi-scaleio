package service

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"github.com/thecodeteam/gocsi"
	sio "github.com/thecodeteam/goscaleio"
	siotypes "github.com/thecodeteam/goscaleio/types/v1"
)

const (
	// Name is the name of the CSI plug-in.
	Name = "csi-scaleio"

	// VendorVersion is the version returned by GetPluginInfo.
	VendorVersion = "0.1.0"

	// EnvEndpoint is the name of the enviroment variable used to set the
	// HTTP endoing of the ScaleIO Gateway
	EnvEndpoint = "X_CSI_SCALEIO_ENDPOINT"

	// EnvUser is the name of the enviroment variable used to set the
	// username when authenticating to the ScaleIO Gateway
	EnvUser = "X_CSI_SCALEIO_USER"

	// EnvPassword is the name of the enviroment variable used to set the
	// user's password when authenticating to the ScaleIO Gateway
	EnvPassword = "X_CSI_SCALEIO_PASSWORD"

	// EnvInsecure is the name of the enviroment variable used to specify
	// that the ScaleIO Gateway's certificate chain and host name should not
	// be verified
	EnvInsecure = "X_CSI_SCALEIO_INSECURE"

	// EnvSystemName is the name of the enviroment variable used to set the
	// name of the ScaleIO system to interact with
	EnvSystemName = "X_CSI_SCALEIO_SYSTEMNAME"

	// EnvSDCGUID is the name of the enviroment variable used to set the
	// GUID of the SDC. This is only used by the Node Service, and removes
	// a need for calling an external binary to retrieve the GUID
	EnvSDCGUID = "X_CSI_SCALEIO_SDCGUID"

	// EnvThick is the name of the enviroment variable used to specify
	// that thick provisioning should be used when creating volumes
	EnvThick = "X_CSI_SCALEIO_THICKPROVISIONING"

	// EnvPrivateDir is the name of the enviroment variable used to specify
	// the path of a private directory used for bind mounting volumes
	EnvPrivateDir = "X_CSI_SCALEIO_PRIVDIR"

	// KeyThickProvisioning is the key used to get a flag indicating that
	// a volume should be thick provisioned from the volume create params
	KeyThickProvisioning = "thickprovisioning"

	thinProvisioned  = "ThinProvisioned"
	thickProvisioned = "ThickProvisioned"
	defaultPrivDir   = "/dev/disk/csi-scaleio"
)

var (
	// SupportedVersions is a list of supported CSI versions.
	SupportedVersions = []*csi.Version{
		&csi.Version{
			Major: 0,
			Minor: 1,
			Patch: 0,
		},
	}
)

// Service is the CSI Mock service provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
	gocsi.IdempotencyProvider
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
func New(
	opts Opts,
	getEnv func(string) string) Service {

	// Environment variables have precedence over all others
	if ep := getEnv(EnvEndpoint); ep != "" {
		opts.Endpoint = ep
	}
	if user := getEnv(EnvUser); user != "" {
		opts.User = user
	}
	if pw := getEnv(EnvPassword); pw != "" {
		opts.Password = pw
	}
	if name := getEnv(EnvSystemName); name != "" {
		opts.SystemName = name
	}
	if guid := getEnv(EnvSDCGUID); guid != "" {
		opts.SdcGUID = guid
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		v := getEnv(n)
		b, err := strconv.ParseBool(v)
		if err != nil {
			log.WithField(n, v).Debug("invalid boolean value. defaulting to false")
			return false
		}
		return b
	}

	opts.Insecure = pb(EnvInsecure)
	opts.Thick = pb(EnvThick)

	privDir := getEnv(EnvPrivateDir)
	if privDir == "" {
		privDir = defaultPrivDir
	}

	fields := map[string]interface{}{
		"endpoint":         opts.Endpoint,
		"user":             opts.User,
		"password":         "",
		"systemname":       opts.SystemName,
		"sdcGUID":          opts.SdcGUID,
		"insecure":         opts.Insecure,
		"thickprovision":   opts.Thick,
		"privatedirectory": privDir,
	}

	if opts.Password != "" {
		fields["password"] = "******"
	}

	log.WithFields(fields).Info("configured ScaleIO parameters")

	s := &service{
		opts:    opts,
		sdcMap:  map[string]string{},
		spCache: map[string]string{},
		privDir: privDir,
	}
	return s
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
