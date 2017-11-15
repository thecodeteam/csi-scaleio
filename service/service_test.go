package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thecodeteam/gocsi"
	"google.golang.org/grpc"

	"github.com/thecodeteam/csi-scaleio/provider"
)

func startServer(ctx context.Context, t *testing.T) (*grpc.ClientConn, func()) {

	// Create a new SP instance and serve it with a piped connection.
	sp := provider.New()
	pipeconn := gocsi.NewPipeConn("csi-test")
	go func() {
		if err := sp.Serve(ctx, pipeconn); err != nil {
			assert.EqualError(t, err, "http: Server closed")
		}
	}()

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDialer(pipeconn.DialGrpc),
	}

	// Create a client for the piped connection.
	client, err := grpc.DialContext(ctx, "", clientOpts...)
	assert.NoError(t, err)

	return client, func() {
		client.Close()
		sp.GracefulStop(ctx)
	}
}
