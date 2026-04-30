package tlsStack

import (
	"fmt"
	"net/netip"
)

func (s *VTLSStack) VTLSDial(
	addr netip.Addr,
	port uint16,
	config VTLSClientConfig,
) (*VTLSConn, error) {
	tcpConn, err := s.tcpStack.VConnect(addr, port)
	if err != nil {
		return nil, err
	}

	tlsConn := &VTLSConn{tcpConn: tcpConn}

	if err := tlsConn.HandleUserKeyExchange(config.UserSignKey, config.ServerVerifyKey); err != nil {
		_ = tcpConn.VClose()
		return nil, err
	}

	return tlsConn, nil
}

func (s *VTLSStack) VTLSListen(
	port uint16,
	config VTLSServerConfig,
) (*VTLSListener, error) {
	tcpListener, err := s.tcpStack.VListen(port)
	if err != nil {
		return nil, err
	}

	return &VTLSListener{
		tcpListener:   tcpListener,
		serverSignKey: config.ServerSignKey,
		userVerifyKey: config.UserVerifyKey,
	}, nil
}

func (l *VTLSListener) VTLSAccept() (*VTLSConn, error) {
	tcpConn, err := l.tcpListener.VAccept()
	if err != nil {
		return nil, err
	}

	conn := &VTLSConn{tcpConn: tcpConn}

	if err := conn.HandleServerKeyExchange(l.serverSignKey, l.userVerifyKey); err != nil {
		_ = tcpConn.VClose()
		return nil, err
	}

	return conn, nil
}

func (c *VTLSConn) VTLSWrite(data []byte) (int, error) {
	return 0, fmt.Errorf("VTLSWrite not implemented yet")
}

func (c *VTLSConn) VTLSRead(buf []byte) (int, error) {
	return 0, fmt.Errorf("VTLSRead not implemented yet")
}

func (c *VTLSConn) VTLSClose() error {
	return c.tcpConn.VClose()
}