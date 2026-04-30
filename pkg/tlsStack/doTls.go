package tlsStack

import (
	"errors"
	"fmt"
	"io"
	"ip-isabelle-and-ben/pkg/tcpStack"
)

func InitVTLSStack(tcp *tcpStack.TCPStack, replChan chan string) *VTLSStack {
	return &VTLSStack{
		tcpStack:     tcp,
		TlsReplChan:  replChan,
	}
}

// -------------------------- TCP MESSAGE HELPERS ----------------------------

// WriteSerializableMessage serializes msg and writes all bytes to the underlying TCP conn.
func WriteSerializableMessage(tcpConn *tcpStack.VTCPConn, msg Serializable) error {
	if tcpConn == nil {
		return errors.New("WriteSerializableMessage: nil tcpConn")
	}
	if msg == nil {
		return errors.New("WriteSerializableMessage: nil msg")
	}

	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	return WriteFull(tcpConn, data)
}

// ReadSerializedMessage reads exactly one serialized handshake message from TCP.
//
// Since your messages are fixed-size and begin with a 1-byte type field, we first
// read the message type, then read the remaining number of bytes for that type.
func ReadSerializedMessage(tcpConn *tcpStack.VTCPConn) ([]byte, error) {
	if tcpConn == nil {
		return nil, errors.New("ReadSerializedMessage: nil tcpConn")
	}

	typeBuf := make([]byte, 1)
	if err := ReadFull(tcpConn, typeBuf); err != nil {
		return nil, err
	}

	msgType := typeBuf[0]

	var remainingLen int

	switch msgType {
	case MessageType_UserToServer_DHPublicValue_Message:
		remainingLen = X25519PublicKeyLen

	case MessageType_ServerToUser_DHPublicValue_Message:
		remainingLen = X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen

	case MessageType_UserToServer_DHSignature_Message:
		remainingLen = X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen

	default:
		return nil, fmt.Errorf("ReadSerializedMessage: unknown message type: %d", msgType)
	}

	body := make([]byte, remainingLen)
	if err := ReadFull(tcpConn, body); err != nil {
		return nil, err
	}

	data := make([]byte, 0, 1+remainingLen)
	data = append(data, typeBuf...)
	data = append(data, body...)

	return data, nil
}

// ReadFull repeatedly calls VRead until exactly len(buf) bytes are filled.
func ReadFull(tcpConn *tcpStack.VTCPConn, buf []byte) error {
	total := 0

	for total < len(buf) {
		n, err := tcpConn.VRead(buf[total:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}

		total += n
	}

	return nil
}

// WriteFull repeatedly calls VWrite until all bytes are written.
func WriteFull(tcpConn *tcpStack.VTCPConn, data []byte) error {
	total := 0

	for total < len(data) {
		n, err := tcpConn.VWrite(data[total:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}

		total += n
	}

	return nil
}