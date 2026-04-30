package tlsStack

import (
	"crypto/ed25519"

	"ip-isabelle-and-ben/pkg/tcpStack"
)

const (
    X25519PublicKeyLen = 32
    Ed25519SignatureLen = 64
    AESGCMKeyLen = 32
)

// -------------------------- TLS STACK / API TYPES ----------------------------

type VTLSStack struct {
	tcpStack *tcpStack.TCPStack
	TlsReplChan chan string
}

type VTLSConn struct {
    tcpConn *tcpStack.VTCPConn

    readKey  []byte // derived sk for reading msgs
    writeKey []byte // derived sk for sending msgs

    readSeq  uint64 // seqNum used to generate nonce to check AES_GCP enc/decryption
    writeSeq uint64
}

type VTLSListener struct {
	tcpListener *tcpStack.VTCPListener

	serverSignKey ed25519.PrivateKey
	userVerifyKey ed25519.PublicKey
}

type VTLSClientConfig struct {
	UserSignKey    ed25519.PrivateKey
	ServerVerifyKey ed25519.PublicKey
}

type VTLSServerConfig struct {
	ServerSignKey ed25519.PrivateKey
	UserVerifyKey ed25519.PublicKey
}

// -------------------------- MESSAGES ----------------------------

const (
	MessageType_UserToServer_DHPublicValue_Message uint8 = 1
	MessageType_ServerToUser_DHPublicValue_Message uint8 = 2
	MessageType_UserToServer_DHSignature_Message   uint8 = 3
)

// enforce serializable iface
type Serializable interface {
	Serialize() ([]byte, error)
	Deserialize([]byte) error
}
var _ Serializable = (*UserToServer_DHPublicValue_Message)(nil)
var _ Serializable = (*ServerToUser_DHPublicValue_Message)(nil)
var _ Serializable = (*UserToServer_DHSignature_Message)(nil)

// user -> server, dhke (1a)
type UserToServer_DHPublicValue_Message struct {
	UserDHPublicValue   []byte  // X25519 public key, 32 bytes
}

// server -> user, dhke (1b)
type ServerToUser_DHPublicValue_Message struct {
	ServerDHPublicValue []byte  // X25519 public key, 32 bytes
	UserDHPublicValue   []byte  // user's X25519 public key, 32 bytes (need to include so user knows the server signed the same DH value the user actually sent)
	ServerSignature     []byte  // Sign(server_dh_public_value || user_dh_public_value)
}

// user -> server, dhke (2a)
type UserToServer_DHSignature_Message struct {
	UserDHPublicValue   []byte  // user's X25519 public key, 32 bytes
	ServerDHPublicValue []byte  // server's X25519 public key, 32 bytes
	UserSignature       []byte  // Sign(user_dh_public_value || server_dh_public_value)
}

// DHKE flow:

// 1. User generates ephemeral X25519 keypair:
//      userDHsk, userDHpk

// 2. User sends:
//      UserToServer_DHPublicValue_Message {
//        user_dh_public_value = userDHpk
//      }

// 3. Server receives userDHpk.

// 4. Server generates ephemeral X25519 keypair:
//      serverDHsk, serverDHpk

// 5. Server signs:
//      Sign(serverDHpk || userDHpk)

// 6. Server sends:
//      ServerToUser_DHPublicValue_Message {
//        server_dh_public_value = serverDHpk
//        user_dh_public_value   = userDHpk
//        server_signature       = Sign(serverDHpk || userDHpk)
//      }

// 7. User verifies:
//      server_signature over serverDHpk || userDHpk

// 8. User signs:
//      Sign(userDHpk || serverDHpk)

// 9. User sends:
//      UserToServer_DHSignature_Message {
//        user_dh_public_value   = userDHpk
//        server_dh_public_value = serverDHpk
//        user_signature         = Sign(userDHpk || serverDHpk)
//      }

// 10. Server verifies:
//       user_signature over userDHpk || serverDHpk

// 11. Both sides compute shared secret:
//       User:   ECDH(userDHsk, serverDHpk)
//       Server: ECDH(serverDHsk, userDHpk)

// 12. Both derive:
//       client_write_key
//       server_write_key