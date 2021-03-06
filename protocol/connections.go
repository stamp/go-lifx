package protocol

import (
	"net"
	"runtime"
	"sync"
	"time"
)

const (
	BroadcastPort   = 56700
	PeerPort        = 56750
	DefaultReadSize = 128
)

type Connection struct {
	Datagrams chan Datagram
	connected bool
	lastErr   error
	sockets   struct {
		broadcast, peer, write *net.UDPConn
	}

	sync.RWMutex
}

type Datagram struct {
	From net.Addr
	Data []byte
}

func (conn Connection) LastError() error {
	conn.RLock()
	defer conn.RUnlock()

	return conn.lastErr
}

func (conn Connection) IsError() bool {
	conn.RLock()
	defer conn.RUnlock()

	return conn.LastError() != nil
}

func (conn Connection) IsConnected() bool {
	conn.RLock()
	defer conn.RUnlock()

	return conn.connected
}

func (conn Connection) Close() (err error) {
	if !conn.IsConnected() {
		return
	}

	err = conn.sockets.broadcast.Close()
	if err != nil {
		return
	}
	err = conn.sockets.peer.Close()
	if err != nil {
		return
	}

	close(conn.Datagrams)

	conn.Lock()
	conn.connected = false
	conn.Unlock()

	return
}

func (conn *Connection) Listen() (<-chan Message, <-chan error) {
	conn.Lock()
	conn.Datagrams = make(chan Datagram)
	conn.Unlock()

	msgs, errs := NewMessageDecoder(conn.Datagrams)
	return msgs, errs
}

func (conn *Connection) WriteMessage(msg Message) (err error) {
	header := Header{
		Version:     1024,
		Site:        [6]byte{0x4c, 0x49, 0x46, 0x58, 0x56, 0x32},
		AtTime:      0,
		Addressable: true,
		Tagged:      true,
		Acknowledge: false,
	}

	if msg.Header != nil {
		header.Target = msg.Header.Target
		header.Tagged = false
	}

	msg.Header = &header

	data, err := msg.MarshalBinary()
	if err != nil {
		return err
	}

	//log.Tracef("SendMessage.header=%#v", msg.Header)
	//log.Tracef("SendMessage.msg=%#v", msg)
	//log.Tracef("SendMessage.length=%d", len(data))

	_, err = conn.write(data)
	if err != nil {
		return err
	}
	<-time.After(time.Millisecond)
	_, err = conn.write(data)
	if err != nil {
		return err
	}
	return nil
}

func (conn *Connection) write(data []byte) (length int, err error) {
	conn.RLock()
	defer conn.RUnlock()

	return conn.sockets.write.Write(data)
}

func (conn *Connection) setupSockets(broadcastAddress string) (err error) {
	// NOTE(bo): On the IP address used for send and receive connections.
	//
	// Go only sets SO_REUSEADDR and SO_REUSEPORT when using a Multicast
	// IP address[1][2][3]. Without dropping down to C, there isn't a way great around
	// this.
	//
	// Despite not being the 255.255.255.255 used in other LIFX libraries, In
	// practice, it seems to work for receiving messages. Sending messages is
	// still somewhat unverified.
	//
	// I may be confusing multicast and broadcast a bit here, but I don't know how else to
	// enable binding to the same interface and port multiple times...
	//
	// [1]: http://golang.org/src/pkg/net/sock_posix.go?h=setDefaultMulticastSockopts#L161
	// [2]: http://golang.org/src/pkg/net/sockopt_bsd.go (also ./sockopt_linux.go)
	// [3]: http://en.wikipedia.org/wiki/Multicast_address#Local_subnetwork
	ip := net.IPv4(224, 0, 0, 1)

	peer, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{
		IP:   ip,
		Port: PeerPort,
	})
	if err != nil {
		return
	}

	broadcast, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{
		IP:   ip,
		Port: BroadcastPort,
	})
	if err != nil {
		return
	}

	if broadcastAddress == "" {
		ip = net.IPv4(255, 255, 255, 255)
	} else {
		ip = net.ParseIP(broadcastAddress)
	}
	write, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   ip,
		Port: BroadcastPort,
	})
	if err != nil {
		return
	}

	conn.Lock()
	conn.sockets.peer = peer
	conn.sockets.broadcast = broadcast
	conn.sockets.write = write
	conn.Unlock()

	return
}

// Starts UDP connections to send and receive datagrams. Returns a Connection struct
// which contains a channel that should be used to receive new UDP packets.
//
// The connection channel will be closed on a socket error. The error can be retrieved
// with the LastError() method on the connection.

func (conn *Connection) Read(socket *net.UDPConn) {
	b := make([]byte, DefaultReadSize)

	for {
		n, addr, err := socket.ReadFrom(b)

		conn.Lock()
		conn.lastErr = err
		conn.Unlock()

		if conn.IsError() {
			close(conn.Datagrams)
			break
		}

		conn.Datagrams <- Datagram{addr, b[0:n]}
	}
}

func Connect(broadcastAddress string) (*Connection, error) {
	conn := &Connection{
		Datagrams: make(chan Datagram),
	}

	err := conn.setupSockets(broadcastAddress)
	if err == nil {
		conn.RLock()
		go conn.Read(conn.sockets.broadcast)
		go conn.Read(conn.sockets.peer)
		conn.RUnlock()
	}

	runtime.SetFinalizer(conn, func(c *Connection) {
		c.Close()
	})

	return conn, err
}
