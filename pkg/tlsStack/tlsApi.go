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
	/* first, if we have leftover decrypted bytes in readBuf, send these first */
	if len(c.readBuf) > 0 {
		n := copy(buf, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	lenBuf := make([]byte, 4)
	/* first, read length field */
	err := ReadFull(c.tcpConn, lenBuf)
	if err != nil {
		return 0, err
	}
	len := binary.BigEndian.Uint32(lenBuf)

	/* make sure auth tag is present */
	if len < AESGCMTagLen {
		return 0, errors.New("VTLSRead: record too short to contain auth tag")
	}

	sealedData := make([]byte, len)
	err = ReadFull(c.tcpConn, sealedData)
	if err != nil {
		return 0, err
	}

	/* decrypt and verify text */
	plaintext, err := c.OpenData(sealedData)
	if err != nil {
		return 0, err
	}

	/* update nonce for next use */
	c.readSeq++
	/* copy plaintext into buf to return */
	n := copy(buf, plaintext)
	/* put whatever we didn't write into the leftover readBuf for next Read */
	c.readBuf = plaintext[n:]
	return n, nil
}

func (c *VTLSConn) VTLSClose() error {
	return c.tcpConn.VClose()
}