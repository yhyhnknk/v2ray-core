package dokodemo

//go:generate errorgen

import (
	"context"
	"time"

	"v2ray.com/core"
	"v2ray.com/core/common"
	"v2ray.com/core/common/buf"
	"v2ray.com/core/common/net"
	"v2ray.com/core/common/session"
	"v2ray.com/core/common/signal"
	"v2ray.com/core/common/task"
	"v2ray.com/core/features/policy"
	"v2ray.com/core/features/routing"
	"v2ray.com/core/transport/internet"
	"v2ray.com/core/transport/pipe"
)

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		d := new(DokodemoDoor)
		err := core.RequireFeatures(ctx, func(pm policy.Manager) error {
			return d.Init(config.(*Config), pm)
		})
		return d, err
	}))
}

type DokodemoDoor struct {
	policyManager policy.Manager
	config        *Config
	address       net.Address
	port          net.Port
}

// Init initializes the DokodemoDoor instance with necessary parameters.
func (d *DokodemoDoor) Init(config *Config, pm policy.Manager) error {
	if (config.NetworkList == nil || len(config.NetworkList.Network) == 0) && len(config.Networks) == 0 {
		return newError("no network specified")
	}
	d.config = config
	d.address = config.GetPredefinedAddress()
	d.port = net.Port(config.Port)
	d.policyManager = pm

	return nil
}

// Network implements proxy.Inbound.
func (d *DokodemoDoor) Network() []net.Network {
	if len(d.config.Networks) > 0 {
		return d.config.Networks
	}

	return d.config.NetworkList.Network
}

func (d *DokodemoDoor) policy() policy.Session {
	config := d.config
	p := d.policyManager.ForLevel(config.UserLevel)
	if config.Timeout > 0 && config.UserLevel == 0 {
		p.Timeouts.ConnectionIdle = time.Duration(config.Timeout) * time.Second
	}
	return p
}

type hasHandshakeAddress interface {
	HandshakeAddress() net.Address
}

// Process implements proxy.Inbound.
func (d *DokodemoDoor) Process(ctx context.Context, network net.Network, conn internet.Connection, dispatcher routing.Dispatcher) error {
	newError("processing connection from: ", conn.RemoteAddr()).AtDebug().WriteToLog(session.ExportIDToError(ctx))
	dest := net.Destination{
		Network: network,
		Address: d.address,
		Port:    d.port,
	}
	if d.config.FollowRedirect {
		if outbound := session.OutboundFromContext(ctx); outbound != nil && outbound.Target.IsValid() {
			dest = outbound.Target
		} else if handshake, ok := conn.(hasHandshakeAddress); ok {
			addr := handshake.HandshakeAddress()
			if addr != nil {
				dest.Address = addr
			}
		}
	}
	if !dest.IsValid() || dest.Address == nil {
		return newError("unable to get destination")
	}

	plcy := d.policy()
	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, plcy.Timeouts.ConnectionIdle)

	ctx = policy.ContextWithBufferPolicy(ctx, plcy.Buffer)
	link, err := dispatcher.Dispatch(ctx, dest)
	if err != nil {
		return newError("failed to dispatch request").Base(err)
	}

	requestDone := func() error {
		defer timer.SetTimeout(plcy.Timeouts.DownlinkOnly)

		reader := buf.NewReader(conn)
		if err := buf.Copy(reader, link.Writer, buf.UpdateActivity(timer)); err != nil {
			return newError("failed to transport request").Base(err)
		}

		return nil
	}

	responseDone := func() error {
		defer timer.SetTimeout(plcy.Timeouts.UplinkOnly)

		var writer buf.Writer
		if network == net.Network_TCP {
			writer = buf.NewWriter(conn)
		} else {
			//if we are in TPROXY mode, use linux's udp forging functionality
			if !d.config.FollowRedirect {
				writer = &buf.SequentialWriter{Writer: conn}
			} else {
				sockopt := &internet.SocketConfig{
					Tproxy: internet.SocketConfig_TProxy,
				}
				if dest.Address.Family().IsIP() {
					sockopt.BindAddress = dest.Address.IP()
					sockopt.BindPort = uint32(dest.Port)
				}
				tConn, err := internet.DialSystem(ctx, net.DestinationFromAddr(conn.RemoteAddr()), sockopt)
				if err != nil {
					return err
				}
				writer = &buf.SequentialWriter{Writer: tConn}
			}
		}

		if err := buf.Copy(link.Reader, writer, buf.UpdateActivity(timer)); err != nil {
			return newError("failed to transport response").Base(err)
		}

		return nil
	}

	if err := task.Run(task.WithContext(ctx),
		task.Parallel(
			task.Single(requestDone, task.OnSuccess(task.Close(link.Writer))),
			responseDone))(); err != nil {
		pipe.CloseError(link.Reader)
		pipe.CloseError(link.Writer)
		return newError("connection ends").Base(err)
	}

	return nil
}
