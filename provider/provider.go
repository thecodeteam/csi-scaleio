package provider

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"github.com/thecodeteam/gocsi"
	"github.com/thecodeteam/goioc"
	"google.golang.org/grpc"

	"github.com/thecodeteam/csi-scaleio/service"
)

const (
	// ReqLoggingEnabled is the name of the environment variable
	// used to determine if the CSI server should enable request
	// logging.
	ReqLoggingEnabled = "X_CSI_SCALEIO_REQ_LOGGING_ENABLED"

	// RepLoggingEnabled is the name of the environment variable
	// used to determine if the CSI server should enable response
	// logging.
	RepLoggingEnabled = "X_CSI_SCALEIO_REP_LOGGING_ENABLED"

	// ReqIDInjectionEnabled is the name of the environment variable
	// used to determine if the CSI server should enable request ID
	// injection.
	ReqIDInjectionEnabled = "X_CSI_SCALEIO_REQ_ID_INJECTION_ENABLED"

	// SpecValidationEnabled is the name of the environment variable
	// used to determine if the CSI server should enable request
	// specification validation.
	SpecValidationEnabled = "X_CSI_SCALEIO_SPEC_VALIDATION_ENABLED"

	// IdempEnabled is the name of the environment variable
	// used to determine if the CSI server should enable idempotency.
	IdempEnabled = "X_CSI_SCALEIO_IDEMPOTENCY_ENABLED"

	// IdempTimeout is the name of the environment variable
	// used to obtain the time.Duration that is the timeout
	// for this plug-in's idempotency provider.
	IdempTimeout = "X_CSI_SCALEIO_IDEMPOTENCY_TIMEOUT"

	// IdempRequireVolume is the name of the environment variable
	// used to determine if the idempotency provider checks to
	// see if a volume exists prior to acting upon it.
	IdempRequireVolume = "X_CSI_SCALEIO_IDEMPOTENCY_REQUIRE_VOLUME"

	// Debug is the name of the environment variable used to determine if
	// debug logging should be enabled
	Debug = "X_CSI_SCALEIO_DEBUG"

	// NodeServiceOnly is the name of the environment variable used to
	// determine if the plugin should host only the Node Service and not
	// the Controller Service
	NodeServiceOnly = "X_CSI_SCALEIO_NODEONLY"

	// ControllerServiceOnly is the name of the environment variable used to
	// determine if the plugin should host only the Controller Service and not
	// the Node Service
	ControllerServiceOnly = "X_CSI_SCALEIO_CONTROLLERONLY"
)

var (
	errServerStopped = errors.New("server stopped")
	errServerStarted = errors.New("server started")

	// ctxConfigKey is an interface-wrapped key used to access a possible
	// config object in the context given to the provider's Serve function
	ctxConfigKey = interface{}("csi.config")

	// ctxOSGetenvKey is an interface-wrapped key used to access a function
	// with the signature func(string)string that returns the value of an
	// environment variable.
	ctxOSGetenvKey = interface{}("os.Getenv")
)

// config is an interface that matches a possible config object that
// could possibly be pulled out of the context given to the provider's
// Serve function
type config interface {
	GetString(key string) string
}

// getEnvFunc is the function signature for os.Getenv.
type getEnvFunc func(string) string

// ServiceProvider is a gRPC endpoint that provides the CSI
// services: Controller, Identity, Node.
type ServiceProvider interface {

	// Serve accepts incoming connections on the listener lis, creating
	// a new ServerTransport and service goroutine for each. The service
	// goroutine read gRPC requests and then call the registered handlers
	// to reply to them. Serve returns when lis.Accept fails with fatal
	// errors.  lis will be closed when this method returns.
	// Serve always returns non-nil error.
	Serve(ctx context.Context, lis net.Listener) error

	// Stop stops the gRPC server. It immediately closes all open
	// connections and listeners.
	// It cancels all active RPCs on the server side and the corresponding
	// pending RPCs on the client side will get notified by connection
	// errors.
	Stop(ctx context.Context)

	// GracefulStop stops the gRPC server gracefully. It stops the server
	// from accepting new connections and RPCs and blocks until all the
	// pending RPCs are finished.
	GracefulStop(ctx context.Context)
}

func init() {
	goioc.Register(service.Name, func() interface{} { return &provider{} })
}

// New returns a new service provider.
func New() ServiceProvider {
	return &provider{}
}

type provider struct {
	sync.Mutex
	server  *grpc.Server
	closed  bool
	service service.Service
}

// Serve accepts incoming connections on the listener lis, creating
// a new ServerTransport and service goroutine for each. The service
// goroutine read gRPC requests and then call the registered handlers
// to reply to them. Serve returns when lis.Accept fails with fatal
// errors.  lis will be closed when this method returns.
// Serve always returns non-nil error.
func (p *provider) Serve(ctx context.Context, li net.Listener) error {

	ge := p.getEnv(ctx)

	// pb parses the string s into a boolean value, always returning a default
	// of false if parsing fails. k is a string that is logged as the source of s
	pb := func(k, s string) bool {
		b, err := strconv.ParseBool(s)
		if err != nil {
			log.WithField(k, s).Debug("invalid boolean value. defaulting to false")
			return false
		}
		return b
	}

	// peb parses an environment variable into a boolean value
	peb := func(e string) bool {
		return pb(e, ge(e))
	}

	if peb(Debug) {
		log.SetLevel(log.DebugLevel)
	}

	opts := service.Opts{}

	if c, ok := ctx.Value(ctxConfigKey).(config); ok {
		opts.Endpoint = c.GetString("csi.scaleio.endpoint")
		opts.User = c.GetString("csi.scaleio.user")
		opts.Password = c.GetString("csi.scaleio.password")
		opts.SystemName = c.GetString("csi.scaleio.systemName")
		opts.SdcGUID = c.GetString("csi.scaleio.sdcGUID")
		opts.Insecure = pb("csi.scaleio.insecure",
			c.GetString("csi.scaleio.insecure"))
		opts.Thick = pb("csi.scaleio.thickProvision",
			c.GetString("csi.scaleio.thickProvision"))
	}

	// Assign the provider a new ScaleIO plug-in.
	p.service = service.New(opts, ge)

	// Create a new gRPC server for serving the storage plug-in.
	if err := func() error {
		p.Lock()
		defer p.Unlock()
		if p.closed {
			return errServerStopped
		}
		if p.server != nil {
			return errServerStarted
		}
		p.server = p.newGrpcServer(ctx, p.service)
		return nil
	}(); err != nil {
		return errServerStarted
	}

	// Register the services.
	// Always host the identity service
	csi.RegisterIdentityServer(p.server, p.service)

	nodeSvc := peb(NodeServiceOnly)
	ctrlSvc := peb(ControllerServiceOnly)

	if nodeSvc && ctrlSvc {
		log.Errorf("Cannot specify both %s and %s",
			NodeServiceOnly, ControllerServiceOnly)
		return fmt.Errorf("Cannot specify both %s and %s",
			NodeServiceOnly, ControllerServiceOnly)
	}
	switch {
	case nodeSvc:
		csi.RegisterNodeServer(p.server, p.service)
		log.Debug("Added Node Service")
	case ctrlSvc:
		csi.RegisterControllerServer(p.server, p.service)
		log.Debug("Added Controller Service")
	default:
		csi.RegisterControllerServer(p.server, p.service)
		log.Debug("Added Controller Service")
		csi.RegisterNodeServer(p.server, p.service)
		log.Debug("Added Node Service")
	}
	// Start the grpc server
	log.WithFields(map[string]interface{}{
		"service": service.Name,
		"address": fmt.Sprintf(
			"%s://%s", li.Addr().Network(), li.Addr().String()),
	}).Info("serving")

	return p.server.Serve(li)
}

// Stop stops the gRPC server. It immediately closes all open
// connections and listeners.
// It cancels all active RPCs on the server side and the corresponding
// pending RPCs on the client side will get notified by connection
// errors.
func (p *provider) Stop(ctx context.Context) {
	if p.server == nil {
		return
	}
	p.Lock()
	defer p.Unlock()
	p.server.Stop()
	p.closed = true
	log.WithField("service", service.Name).Info("stopped")
}

// GracefulStop stops the gRPC server gracefully. It stops the server
// from accepting new connections and RPCs and blocks until all the
// pending RPCs are finished.
func (p *provider) GracefulStop(ctx context.Context) {
	if p.server == nil {
		return
	}
	p.Lock()
	defer p.Unlock()
	p.server.GracefulStop()
	p.closed = true
	log.WithField("service", service.Name).Info("shutdown")
}

func (p *provider) newGrpcServer(
	ctx context.Context,
	i gocsi.IdempotencyProvider) *grpc.Server {

	// Get the function used to query environment variables.
	ge := p.getEnv(ctx)

	// Create the server-side interceptor chain option.
	iceptors := newGrpcInterceptors(ctx, i, ge)
	iceptorChain := gocsi.ChainUnaryServer(iceptors...)
	iceptorOpt := grpc.UnaryInterceptor(iceptorChain)

	return grpc.NewServer(iceptorOpt)
}

func (p *provider) getEnv(
	ctx context.Context) getEnvFunc {

	// Get the function used to query environment variables.
	var getEnv = os.Getenv
	if f, ok := ctx.Value(ctxOSGetenvKey).(getEnvFunc); ok {
		getEnv = f
	}
	return getEnv
}

func newGrpcInterceptors(
	ctx context.Context,
	idemp gocsi.IdempotencyProvider,
	getEnv getEnvFunc) []grpc.UnaryServerInterceptor {

	// pb parses an environment variable into a boolean value.
	pb := func(n string) bool {
		b, err := strconv.ParseBool(getEnv(n))
		if err != nil {
			return true
		}
		return b
	}

	var (
		usi           []grpc.UnaryServerInterceptor
		reqLogEnabled = pb(ReqLoggingEnabled)
		repLogEnabled = pb(RepLoggingEnabled)
		reqIDEnabled  = pb(ReqIDInjectionEnabled)
		specEnabled   = pb(SpecValidationEnabled)
		idempEnabled  = pb(IdempEnabled)
		idempReqVol   = pb(IdempRequireVolume)
	)

	if reqIDEnabled {
		usi = append(usi, gocsi.NewServerRequestIDInjector())
	}

	// If request or response logging are enabled then create the loggers.
	if reqLogEnabled || repLogEnabled {
		var (
			opts []gocsi.LoggingOption
			lout = newLogger(log.Infof)
		)
		if reqLogEnabled {
			opts = append(opts, gocsi.WithRequestLogging(lout))
		}
		if repLogEnabled {
			opts = append(opts, gocsi.WithResponseLogging(lout))
		}
		usi = append(usi, gocsi.NewServerLogger(opts...))
	}

	if specEnabled {
		sv := make([]csi.Version, len(service.SupportedVersions))
		for i, v := range service.SupportedVersions {
			sv[i] = *v
		}
		usi = append(
			usi,
			gocsi.NewServerSpecValidator(
				gocsi.WithSupportedVersions(sv...),
				gocsi.WithSuccessDeleteVolumeNotFound(),
				gocsi.WithSuccessCreateVolumeAlreadyExists(),
				gocsi.WithRequiresNodeID(),
			),
		)
	}

	if idempEnabled {
		// Get idempotency provider's timeout.
		timeout, _ := time.ParseDuration(getEnv(IdempTimeout))

		iopts := []gocsi.IdempotentInterceptorOption{
			gocsi.WithIdempTimeout(timeout),
		}

		// Check to see if the idempotency provider requires volumes to exist.
		if idempReqVol {
			iopts = append(iopts, gocsi.WithIdempRequireVolumeExists())
		}

		usi = append(usi, gocsi.NewIdempotentInterceptor(idemp, iopts...))
	}

	return usi
}

type logger struct {
	f func(msg string, args ...interface{})
	w io.Writer
}

func newLogger(f func(msg string, args ...interface{})) *logger {
	l := &logger{f: f}
	r, w := io.Pipe()
	l.w = w
	go func() {
		scan := bufio.NewScanner(r)
		for scan.Scan() {
			f(scan.Text())
		}
	}()
	return l
}

func (l *logger) Write(data []byte) (int, error) {
	return l.w.Write(data)
}
