package client

import (
	"net"
	"runtime"

	"fmt"

	proto "github.com/stamp/go-lifx/protocol"
)

const (
	LampAdded   = 1
	LampUpdated = 2
	LampRemoved = 3

	DEFAULT_KELVIN = 3500
)

type LampCollectionEvent struct {
	Event int
	Lamp  *Light
}

type LightCollection struct {
	*Client
	lights  map[string]*Light
	Changes chan LampCollectionEvent
}

func (lc *LightCollection) Register(from net.Addr, header *proto.Header, state *proto.LightState) *Light {

	if existing, ok := lc.lights[header.Target]; !ok {
		light := &Light{Client: lc.Client}
		light.UpdateFromState(state)
		light.ip = from
		light.id = header.Target
		lc.lights[header.Target] = light

		// Try to notify that a new lamp was added to the collection
		select {
		case lc.Changes <- LampCollectionEvent{LampAdded, light}:
		default:
		}
	} else {
		existing.ip = from
	}

	return lc.lights[header.Target]
}

func (lc *LightCollection) Count() int {
	return len(lc.lights)
}

func (lc *LightCollection) All() map[string]*Light {
	return lc.lights
}

func (lc *LightCollection) GetById(id string) (*Light, error) {
	if existing, ok := lc.lights[id]; ok {
		return existing, nil
	}

	return nil, fmt.Errorf("Could not find %s in list of lamps", id)
}

type Light struct {
	*Client
	state *proto.LightState

	Datagrams chan proto.Datagram
	connected bool
	id        string
	ip        net.Addr
	conn      *net.UDPConn
}

func (l *Light) Label() string {
	return l.state.Label.String()
}

func (l *Light) Id() string {
	return l.id
}

func (l *Light) UpdateFromState(state *proto.LightState) {
	l.state = state
}

func (l Light) TurnOff() {
	l.Client.SendMessage(proto.DeviceSetPower{Level: 0})
}

func (l Light) TurnOn() {
	l.Client.SendMessage(proto.DeviceSetPower{Level: 1})
}

// Note that h is in [0..360] and s,v in [0..1]
func (l Light) SetState(h, s, v float64, duration uint32, kelvin uint32) {

	l.Client.SendMessage(proto.LightSet{
		Color: proto.Hsbk{
			Hue:        proto.Degrees(h / 360 * 65535), // 0-65535 scaled to 0-100%
			Saturation: proto.Percent(s * 65535),       // 0-65535 scaled to 0-100%
			Brightness: proto.Percent(v * 65535),       // 0-65535 scaled to 0-100%
			Kelvin:     proto.Kelvin(kelvin),           // absolute 2400-10000
		},
		Duration: duration,
	})

}

func (l Light) IsConnected() bool {
	return l.connected
}

func (l Light) Close() (err error) {
	if !l.IsConnected() {
		return
	}

	err = l.conn.Close()
	if err != nil {
		return
	}

	l.connected = false
	return
}

func (l Light) Listen() (<-chan proto.Message, <-chan error) {
	l.Datagrams = make(chan proto.Datagram)
	msgs, errs := proto.NewMessageDecoder(l.Datagrams)
	return msgs, errs
}

func (l Light) Connect() error {
	l.Datagrams = make(chan proto.Datagram)

	var err error
	l.conn, err = net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{
		IP:   net.ParseIP(l.ip.String()),
		Port: proto.PeerPort,
	})
	if err != nil {
		return err
	}

	runtime.SetFinalizer(l.conn, func(c *net.UDPConn) {
		c.Close()
	})

	return err
}
