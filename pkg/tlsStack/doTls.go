package tlsStack

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"ip-isabelle-and-ben/pkg/tcpStack"
)

func InitVTLSStack(tcp *tcpStack.TCPStack, replChan chan string) *VTLSStack {
	return &VTLSStack{
		tcpStack:     tcp,
		TlsReplChan:  replChan,
		tlsConnMap:   make(map[int]*VTLSConn), /* map to store conns */
	}
}

/* 
Post-handshake: sending messages
- both sides have write and read key (32-byte AES-256-GCM keys)
	- preserves confidentiality + integry (contains 16-byte authentication tag alongside cyphertext)
	- reader verifies tag before reading message to ensure message not tampered with or falsified
		- protects against attacker flipping bits in cypertext, etc

- nonce (number used once)
	- 12-byte value used each time you encrypt with the same key that has to be different
		- writeSeq and readSeq
		- start at 0 and increment by 1 each message

- writing data:
	1. get nonce
		- make writeSeq an 8-byte integer and pad to get 12 bytes
	2. use cipher.AEAD.Seal to encrypt message with writeKey
	3. create bytes to send
	4. call WriteFull to write to TCP
	5. increment writeSeq

- reading data:
	1. knowing we got a Record, first read length (ReadFull for 4 bytes)
	2. read Record ciphertext + authTag
	3. get nonce via same method as write
	4. decrypt message via cipher.AEAD.Open (pass in readKey + nonce)
		- atomically: verifies auth tag. returns error if wrong
		- then returns plaintext if everything works
	5. inrement readSeq

*/

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

/* convert nonce into expected 12-byte array format */
func getNonce(seq uint64) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}