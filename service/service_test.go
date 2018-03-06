package service_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/akutz/memconn"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/thecodeteam/csi-scaleio/provider"
)

func startServer(ctx context.Context, t *testing.T) (*grpc.ClientConn, func()) {

	// Create a new SP instance and serve it with a piped connection.
	sp := provider.New()
	lis, err := memconn.Listen("csi-test")
	assert.NoError(t, err)
	go func() {
		if err := sp.Serve(ctx, lis); err != nil {
			assert.EqualError(t, err, "http: Server closed")
		}
	}()

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
			return memconn.Dial("csi-test")
		}),
	}

	// Create a client for the piped connection.
	client, err := grpc.DialContext(ctx, "", clientOpts...)
	assert.NoError(t, err)

	return client, func() {
		client.Close()
		sp.GracefulStop(ctx)
	}
}
