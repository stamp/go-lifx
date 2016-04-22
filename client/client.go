package client

import (
	"time"

	log "github.com/cihub/seelog"
	proto "github.com/stamp/go-lifx/protocol"
)

type connection interface {
	Listen() (<-chan proto.Message, <-chan error)
	WriteMessage(proto.Message) error
}

type Client struct {
	connected  bool
	connection connection
	Messages   <-chan proto.Message
	Errors     <-chan error
	Lights     *LightCollection
}

func New() *Client {
	c := &Client{}
	if conn, err := proto.Connect(); err == nil {
		c.connection = conn
		c.connected = true
	}

	messages, errors := c.connection.Listen()

	c.Messages = messages
	c.Errors = errors
	c.Lights = &LightCollection{
		Client:  c,
		lights:  make(map[string]*Light, 0),
		Changes: make(chan LampCollectionEvent, 0),
	}

	// Listen worker
	go func() {
		for {
			select {
			case msg := <-c.Messages:
				switch payload := msg.Payload.(type) {
				case *proto.DeviceStatePanGateway:
					// TODO: record gateway devices
					//log.Warn("Received: ", reflect.TypeOf(msg.Payload))
				case *proto.LightState:
					//log.Warn("Register: ", reflect.TypeOf(msg.Payload))
					c.Lights.Register(msg.From, msg.Header, payload)
				default:
					//log.Warn("Received: ", reflect.TypeOf(msg.Payload))
					// nada
				}
			}
		}
	}()

	return c
}

func (c *Client) SendMessage(payload proto.Payload) (data []byte, error error) {
	log.Debugf("SendMessage(%#v)", payload)
	msg := proto.Message{}
	msg.Payload = payload

	c.connection.WriteMessage(msg)
	return data, nil
}

func (c *Client) Discover() <-chan *Light {
	ch := make(chan *Light)

	go func() {
		timeout := time.After(5 * time.Second)

		c.SendMessage(proto.LightGet{})

		for {
			select {
			case <-timeout:
				close(ch)
				return

			}
		}
	}()

	return ch
}
