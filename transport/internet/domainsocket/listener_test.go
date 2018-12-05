// +build !windows

package domainsocket_test

import (
	"context"
	"runtime"
	"testing"

	"v2ray.com/core/common"
	"v2ray.com/core/common/buf"
	"v2ray.com/core/common/net"
	"v2ray.com/core/transport/internet"
	. "v2ray.com/core/transport/internet/domainsocket"
	. "v2ray.com/ext/assert"
)

func TestListen(t *testing.T) {
	assert := With(t)

	ctx := context.Background()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "domainsocket",
		ProtocolSettings: &Config{
			Path: "/tmp/ts3",
		},
	}
	listener, err := Listen(ctx, nil, net.Port(0), streamSettings, func(conn internet.Connection) {
		defer conn.Close()

		b := buf.New()
		common.Must2(b.ReadFrom(conn))
		assert(b.String(), Equals, "Request")

		common.Must2(conn.Write([]byte("Response")))
	})
	assert(err, IsNil)
	defer listener.Close()

	conn, err := Dial(ctx, net.Destination{}, streamSettings)
	assert(err, IsNil)
	defer conn.Close()

	_, err = conn.Write([]byte("Request"))
	assert(err, IsNil)

	b := buf.New()
	common.Must2(b.ReadFrom(conn))

	assert(b.String(), Equals, "Response")
}

func TestListenAbstract(t *testing.T) {
	if runtime.GOOS != "linux" {
		return
	}

	assert := With(t)

	ctx := context.Background()
	streamSettings := &internet.MemoryStreamConfig{
		ProtocolName: "domainsocket",
		ProtocolSettings: &Config{
			Path:     "/tmp/ts3",
			Abstract: true,
		},
	}
	listener, err := Listen(ctx, nil, net.Port(0), streamSettings, func(conn internet.Connection) {
		defer conn.Close()

		b := buf.New()
		common.Must2(b.ReadFrom(conn))
		assert(b.String(), Equals, "Request")

		common.Must2(conn.Write([]byte("Response")))
	})
	assert(err, IsNil)
	defer listener.Close()

	conn, err := Dial(ctx, net.Destination{}, streamSettings)
	assert(err, IsNil)
	defer conn.Close()

	_, err = conn.Write([]byte("Request"))
	assert(err, IsNil)

	b := buf.New()
	common.Must2(b.ReadFrom(conn))

	assert(b.String(), Equals, "Response")
}
