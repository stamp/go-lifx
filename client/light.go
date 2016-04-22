package client

import (
	"net"
	"runtime"

	"fmt"

	log "github.com/cihub/seelog"

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

	id := header.Target
	//log.Debugf("Register call for %s (%s) in collection (%d)", id, header.Target, len(lc.lights))

	if existing, ok := lc.lights[id]; !ok {
		log.Infof("Adding light: %s", from.String())
		light := &Light{Client: lc.Client}
		light.UpdateFromState(state)
		light.Ip = from
		light.id = id
		lc.lights[id] = light

		// Try to notify that a new lamp was added to the collection
		select {
		case lc.Changes <- LampCollectionEvent{LampAdded, light}:
		default:
			log.Warnf("Lost light event: %s", from.String())
		}
	} else {
		//log.Infof("Updating ip: %s", from.String())

		select {
		case lc.Changes <- LampCollectionEvent{LampUpdated, existing}:
		default:
			log.Warnf("Lost light event: %s", from.String())
		}

		existing.Ip = from
	}

	return lc.lights[id]
}

func (lc *LightCollection) Count() int {
	return len(lc.lights)
}

func (lc *LightCollection) All() map[string]*Light {
	return lc.lights
}

func (lc *LightCollection) GetById(id string) (*Light, error) {
	if existing, ok := lc.lights[id]; ok {
		//log.Debugf("GetById(%s) returned %s", id, existing.Id())
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
	Ip        net.Addr
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
	l.SendMessage(proto.DeviceSetPower{Level: 0})
}

func (l Light) TurnOn() {
	l.SendMessage(proto.DeviceSetPower{Level: 1})
}

// Note that h is in [0..360] and s,v in [0..1]
func (l Light) SetState(h, s, v float64, duration uint32, kelvin uint32) {

	l.SendMessage(proto.LightSet{
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
		IP:   net.ParseIP(l.Ip.String()),
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

func (l *Light) SendMessage(payload proto.Payload) (data []byte, error error) {
	//log.Debugf("(%p).SendMessage(%#v) (%s)", l, payload, l.Id())

	msg := proto.Message{
		Header: &proto.Header{
			Target: l.Id(),
		},
		Payload: payload,
	}

	//log.Debugf("(%p).SendMessage(%#v) msg.Target=%v -> %v", l, payload, "asd", l.id)
	//log.Debugf("SendMessage(%#v)", msg.Header)

	l.connection.WriteMessage(msg)
	return data, nil
}
