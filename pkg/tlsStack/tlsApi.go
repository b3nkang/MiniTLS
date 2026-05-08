package tlsStack

import (
	"encoding/binary"
	"errors"
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

/* turn plaintext into ciphertext + auth and write to TCP conn */
func (c *VTLSConn) VTLSWrite(data []byte) (int, error) {
	/* encrypt and add auth to data */
	sealedData, err := c.SealData(data)
	if err != nil {
		return 0, err
	}

	/* make length field to send with data so reader knows what to decrypt */
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(sealedData)))

	/* first, write length field */
	err = WriteFull(c.tcpConn, lenBuf)
	if err != nil {
		return 0, err
	}
	/* then, write data */
	err = WriteFull(c.tcpConn, sealedData)
	if err != nil {
		return 0, err
	}

	/* increment nonce for next use */
	c.writeSeq++

	/* return length of plaintext */
	return len(data), nil
}

/* translate ciphertext + auth after reading and verifying auth from TCP conn */
func (c *VTLSConn) VTLSRead(buf []byte) (int, error) {
	/* if leftover bytes from a previous record exist, return them immediately — no blocking */
	if len(c.readBuf) > 0 {
		n := copy(buf, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	/* no buffered data — block for exactly one record */
	lenBuf := make([]byte, 4)
	if err := ReadFull(c.tcpConn, lenBuf); err != nil {
		return 0, err
	}
	recordLen := binary.BigEndian.Uint32(lenBuf)

	if recordLen < AESGCMTagLen {
		return 0, errors.New("VTLSRead: record too short to contain auth tag")
	}

	sealedData := make([]byte, recordLen)
	if err := ReadFull(c.tcpConn, sealedData); err != nil {
		return 0, err
	}

	plaintext, err := c.OpenData(sealedData)
	if err != nil {
		return 0, err
	}

	c.readSeq++
	n := copy(buf, plaintext)
	c.readBuf = plaintext[n:]
	return n, nil
}

func (c *VTLSConn) VTLSClose() error {
	return c.tcpConn.VClose()
}