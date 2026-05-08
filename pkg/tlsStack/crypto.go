package tlsStack

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// -------------------------- Bootstrapping ----------------------------

func LoadTestTlsConfigs() (VTLSClientConfig, VTLSServerConfig) {
	userSeed := sha256.Sum256([]byte("minitls test user signing key"))
	serverSeed := sha256.Sum256([]byte("minitls test server signing key"))

	userPriv := ed25519.NewKeyFromSeed(userSeed[:])
	serverPriv := ed25519.NewKeyFromSeed(serverSeed[:])

	userPub := userPriv.Public().(ed25519.PublicKey)
	serverPub := serverPriv.Public().(ed25519.PublicKey)

	clientConfig := VTLSClientConfig{
		UserSignKey:     userPriv,
		ServerVerifyKey: serverPub,
	}

	serverConfig := VTLSServerConfig{
		ServerSignKey: serverPriv,
		UserVerifyKey: userPub,
	}

	return clientConfig, serverConfig
}


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

func (c *VTLSConn) PrintHandshakeDebug(role string) {
	readHash := sha256.Sum256(c.readKey)
	writeHash := sha256.Sum256(c.writeKey)

	fmt.Printf("[TLS %s] handshake complete\n", role)
	fmt.Printf("[TLS %s] readKey hash:  %x\n", role, readHash[:8])
	fmt.Printf("[TLS %s] writeKey hash: %x\n", role, writeHash[:8])
}

/* build nonce, create ciphertext, and call Seal to return ciphertext || auth */
func (c *VTLSConn) SealData(plaintext []byte) ([]byte, error) {
	nonce := getNonce(c.writeSeq)

	/* create new cipher block -- knows how to encrypt chunks of data */
	block, err := aes.NewCipher(c.writeKey)
	if err != nil {
		return nil, err
	}

	/* returns cipher.AEAD -> Authenticated Encryption with Assoicated Data 
		object we use to encrypt data */
	gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

	/* encrypt data and append auth */
	sealedData := gcm.Seal(nil, nonce, plaintext, nil)
	
	return sealedData, nil
}

func (c *VTLSConn) OpenData(sealedData []byte) ([]byte, error) {
	nonce := getNonce(c.readSeq)

	/* create new cipher block -- knows how to encrypt chunks of data */
	block, err := aes.NewCipher(c.readKey)
	if err != nil {
		return nil, err
	}

	/* returns cipher.AEAD -> Authenticated Encryption with Assoicated Data 
		object we use to encrypt data */
	gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

	plaintext, err := gcm.Open(nil, nonce, sealedData, nil)
	if err != nil {
		return nil, fmt.Errorf("VTLSRead: decryption/auth failed: %w", err)
	}
	return plaintext, nil
}