package tls

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"ip-isabelle-and-ben/pkg/tcpStack"

	"golang.org/x/crypto/hkdf"
)

// -------------------------- CRYPTO HELPERS ----------------------------

// GenerateDHPair generates an ephemeral X25519 keypair.
//
// Returns:
//   - private key object
//   - public key bytes
func GenerateDHPair() (*ecdh.PrivateKey, []byte, error) {
	curve := ecdh.X25519()

	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	pub := priv.PublicKey().Bytes()
	return priv, pub, nil
}

// ComputeSharedSecret computes X25519 ECDH using our private key and peer public key bytes.
func ComputeSharedSecret(priv *ecdh.PrivateKey, peerPubBytes []byte) ([]byte, error) {
	if len(peerPubBytes) != X25519PublicKeyLen {
		return nil, fmt.Errorf("ComputeSharedSecret: invalid peer public key length: got %d, want %d",
			len(peerPubBytes), X25519PublicKeyLen)
	}

	curve := ecdh.X25519()

	peerPub, err := curve.NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, err
	}

	sharedSecret, err := priv.ECDH(peerPub)
	if err != nil {
		return nil, err
	}

	return sharedSecret, nil
}

// ServerSignedDHPublicValues returns the exact byte string the server signs:
//
//	server_dh_public_value || user_dh_public_value
func ServerSignedDHPublicValues(serverDHPub []byte, userDHPub []byte) []byte {
	out := make([]byte, 0, len(serverDHPub)+len(userDHPub))
	out = append(out, serverDHPub...)
	out = append(out, userDHPub...)
	return out
}

// UserSignedDHPublicValues returns the exact byte string the user/client signs:
//
//	user_dh_public_value || server_dh_public_value
func UserSignedDHPublicValues(userDHPub []byte, serverDHPub []byte) []byte {
	out := make([]byte, 0, len(userDHPub)+len(serverDHPub))
	out = append(out, userDHPub...)
	out = append(out, serverDHPub...)
	return out
}

// DeriveWriteKeys derives two AES-GCM keys from the DH shared secret.
//
// Output convention:
//   - clientWriteKey encrypts user/client -> server records
//   - serverWriteKey encrypts server -> user/client records
func DeriveWriteKeys(
	sharedSecret []byte,
	userDHPub []byte,
	serverDHPub []byte,
) (clientWriteKey []byte, serverWriteKey []byte, err error) {
	if len(sharedSecret) == 0 {
		return nil, nil, errors.New("DeriveWriteKeys: empty shared secret")
	}
	if len(userDHPub) != X25519PublicKeyLen {
		return nil, nil, fmt.Errorf("DeriveWriteKeys: invalid user DH public key length: got %d, want %d",
			len(userDHPub), X25519PublicKeyLen)
	}
	if len(serverDHPub) != X25519PublicKeyLen {
		return nil, nil, fmt.Errorf("DeriveWriteKeys: invalid server DH public key length: got %d, want %d",
			len(serverDHPub), X25519PublicKeyLen)
	}

	// Use the DH public values as salt/context so both parties derive keys bound to this exchange.
	h := sha256.New()
	h.Write(userDHPub)
	h.Write(serverDHPub)
	salt := h.Sum(nil)

	reader := hkdf.New(
		sha256.New,
		sharedSecret,
		salt,
		[]byte("write keys"),
	)

	keyMaterial := make([]byte, 2*AESGCMKeyLen)
	if _, err := io.ReadFull(reader, keyMaterial); err != nil {
		return nil, nil, err
	}

	clientWriteKey = append([]byte(nil), keyMaterial[:AESGCMKeyLen]...)
	serverWriteKey = append([]byte(nil), keyMaterial[AESGCMKeyLen:]...)

	return clientWriteKey, serverWriteKey, nil
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