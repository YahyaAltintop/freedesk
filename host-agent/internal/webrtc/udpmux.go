package webrtc

import (
	"fmt"
	"net"

	"github.com/pion/ice/v4"
)

// UDPMux is the one UDP socket every session's ICE traffic goes through.
//
// Opening it once, first thing at start-up, does two things a socket per
// session cannot. Windows asks whether to let a program through its firewall
// the moment the program first listens; with a socket per session that moment
// came behind the first connection's consent prompt, where the question was
// easy to miss and, left unanswered, kept every packet from the viewer out.
// Now it comes while the operator is still looking at the window. And a fixed
// port is something a person can put in a firewall rule.
type UDPMux struct {
	conn *net.UDPConn
	mux  *ice.UDPMuxDefault
}

// ListenUDP opens the socket on port, or on any free port when that one is
// taken (port 0 asks for any port outright).
func ListenUDP(port int) (*UDPMux, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil && port != 0 {
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	}
	if err != nil {
		return nil, fmt.Errorf("could not open a UDP socket: %w", err)
	}
	return &UDPMux{conn: conn, mux: ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: conn})}, nil
}

// Port is the port the socket ended up on.
func (m *UDPMux) Port() int {
	return m.conn.LocalAddr().(*net.UDPAddr).Port
}

// Mux is what a Config carries.
func (m *UDPMux) Mux() ice.UDPMux { return m.mux }

// Close releases the socket. Only once every session is over: a peer built
// on the mux cannot outlive it.
func (m *UDPMux) Close() error {
	err := m.mux.Close()
	_ = m.conn.Close()
	return err
}
