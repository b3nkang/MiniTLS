package tls

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
)

// -------------------------- PUBLIC HANDSHAKE ENTRYPOINTS ----------------------------

// HandleUserKeyExchange runs the user/client side of the authenticated DHKE.
//
// Flow:
//   1. Generate user X25519 DH keypair
//   2. Send UserToServer_DHPublicValue_Message
//   3. Receive ServerToUser_DHPublicValue_Message
//   4. Verify server signature over serverDHPub || userDHPub
//   5. Send UserToServer_DHSignature_Message
//   6. Compute shared secret
//   7. Derive client/server write keys
//   8. Store readKey/writeKey on VTLSConn
func (conn *VTLSConn) HandleUserKeyExchange(
	userSignKey ed25519.PrivateKey,
	serverVerifyKey ed25519.PublicKey,
) error {
	if conn == nil || conn.tcpConn == nil {
		return errors.New("HandleUserKeyExchange: nil VTLSConn or tcpConn")
	}

	// 1. Generate ephemeral user/client X25519 keypair.
	userDHPriv, userDHPub, err := GenerateDHPair()
	if err != nil {
		return fmt.Errorf("HandleUserKeyExchange: generate user DH pair: %w", err)
	}

	// 2. Send user DH public value.
	userDHMsg := &UserToServer_DHPublicValue_Message{
		UserDHPublicValue: userDHPub,
	}

	if err := WriteSerializableMessage(conn.tcpConn, userDHMsg); err != nil {
		return fmt.Errorf("HandleUserKeyExchange: send user DH public value: %w", err)
	}

	// 3. Receive server DH public value + server signature.
	rawServerMsg, err := ReadSerializedMessage(conn.tcpConn)
	if err != nil {
		return fmt.Errorf("HandleUserKeyExchange: read server DH message: %w", err)
	}

	serverDHMsg := &ServerToUser_DHPublicValue_Message{}
	if err := serverDHMsg.Deserialize(rawServerMsg); err != nil {
		return fmt.Errorf("HandleUserKeyExchange: deserialize server DH message: %w", err)
	}

	// Make sure server echoed the same user DH public value we sent.
	if !bytes.Equal(serverDHMsg.UserDHPublicValue, userDHPub) {
		return errors.New("HandleUserKeyExchange: server echoed wrong user DH public value")
	}

	// 4. Verify server signature over serverDHPub || userDHPub.
	serverSignedData := ServerSignedDHPublicValues(
		serverDHMsg.ServerDHPublicValue,
		serverDHMsg.UserDHPublicValue,
	)

	if !ed25519.Verify(serverVerifyKey, serverSignedData, serverDHMsg.ServerSignature) {
		return errors.New("HandleUserKeyExchange: server signature verification failed")
	}

	// 5. Sign userDHPub || serverDHPub and send final user signature message.
	userSignedData := UserSignedDHPublicValues(
		userDHPub,
		serverDHMsg.ServerDHPublicValue,
	)

	userSig := ed25519.Sign(userSignKey, userSignedData)

	userSigMsg := &UserToServer_DHSignature_Message{
		UserDHPublicValue:   userDHPub,
		ServerDHPublicValue: serverDHMsg.ServerDHPublicValue,
		UserSignature:       userSig,
	}

	if err := WriteSerializableMessage(conn.tcpConn, userSigMsg); err != nil {
		return fmt.Errorf("HandleUserKeyExchange: send user signature message: %w", err)
	}

	// 6. Compute shared secret: ECDH(userDHPriv, serverDHPub).
	sharedSecret, err := ComputeSharedSecret(userDHPriv, serverDHMsg.ServerDHPublicValue)
	if err != nil {
		return fmt.Errorf("HandleUserKeyExchange: compute shared secret: %w", err)
	}

	// 7. Derive directional keys.
	clientWriteKey, serverWriteKey, err := DeriveWriteKeys(
		sharedSecret,
		userDHPub,
		serverDHMsg.ServerDHPublicValue,
	)
	if err != nil {
		return fmt.Errorf("HandleUserKeyExchange: derive write keys: %w", err)
	}

	// 8. User/client writes with clientWriteKey, reads with serverWriteKey.
	conn.writeKey = clientWriteKey
	conn.readKey = serverWriteKey
	conn.writeSeq = 0
	conn.readSeq = 0

	return nil
}

// HandleServerKeyExchange runs the server side of the authenticated DHKE.
//
// Flow:
//   1. Receive UserToServer_DHPublicValue_Message
//   2. Generate server X25519 DH keypair
//   3. Sign serverDHPub || userDHPub
//   4. Send ServerToUser_DHPublicValue_Message
//   5. Receive UserToServer_DHSignature_Message
//   6. Verify user signature over userDHPub || serverDHPub
//   7. Compute shared secret
//   8. Derive client/server write keys
//   9. Store readKey/writeKey on VTLSConn
func (conn *VTLSConn) HandleServerKeyExchange(
	serverSignKey ed25519.PrivateKey,
	userVerifyKey ed25519.PublicKey,
) error {
	if conn == nil || conn.tcpConn == nil {
		return errors.New("HandleServerKeyExchange: nil VTLSConn or tcpConn")
	}

	// 1. Receive user/client DH public value.
	rawUserMsg, err := ReadSerializedMessage(conn.tcpConn)
	if err != nil {
		return fmt.Errorf("HandleServerKeyExchange: read user DH message: %w", err)
	}

	userDHMsg := &UserToServer_DHPublicValue_Message{}
	if err := userDHMsg.Deserialize(rawUserMsg); err != nil {
		return fmt.Errorf("HandleServerKeyExchange: deserialize user DH message: %w", err)
	}

	userDHPub := userDHMsg.UserDHPublicValue

	// 2. Generate ephemeral server X25519 keypair.
	serverDHPriv, serverDHPub, err := GenerateDHPair()
	if err != nil {
		return fmt.Errorf("HandleServerKeyExchange: generate server DH pair: %w", err)
	}

	// 3. Server signs serverDHPub || userDHPub.
	serverSignedData := ServerSignedDHPublicValues(serverDHPub, userDHPub)
	serverSig := ed25519.Sign(serverSignKey, serverSignedData)

	// 4. Send server DH public value, echoed user DH public value, and server signature.
	serverDHMsg := &ServerToUser_DHPublicValue_Message{
		ServerDHPublicValue: serverDHPub,
		UserDHPublicValue:   userDHPub,
		ServerSignature:     serverSig,
	}

	if err := WriteSerializableMessage(conn.tcpConn, serverDHMsg); err != nil {
		return fmt.Errorf("HandleServerKeyExchange: send server DH message: %w", err)
	}

	// 5. Receive user's signature message.
	rawUserSigMsg, err := ReadSerializedMessage(conn.tcpConn)
	if err != nil {
		return fmt.Errorf("HandleServerKeyExchange: read user signature message: %w", err)
	}

	userSigMsg := &UserToServer_DHSignature_Message{}
	if err := userSigMsg.Deserialize(rawUserSigMsg); err != nil {
		return fmt.Errorf("HandleServerKeyExchange: deserialize user signature message: %w", err)
	}

	// Sanity-check that the user is signing the same public values from this exchange.
	if !bytes.Equal(userSigMsg.UserDHPublicValue, userDHPub) {
		return errors.New("HandleServerKeyExchange: user signature message contains wrong user DH public value")
	}
	if !bytes.Equal(userSigMsg.ServerDHPublicValue, serverDHPub) {
		return errors.New("HandleServerKeyExchange: user signature message contains wrong server DH public value")
	}

	// 6. Verify user signature over userDHPub || serverDHPub.
	userSignedData := UserSignedDHPublicValues(
		userSigMsg.UserDHPublicValue,
		userSigMsg.ServerDHPublicValue,
	)

	if !ed25519.Verify(userVerifyKey, userSignedData, userSigMsg.UserSignature) {
		return errors.New("HandleServerKeyExchange: user signature verification failed")
	}

	// 7. Compute shared secret: ECDH(serverDHPriv, userDHPub).
	sharedSecret, err := ComputeSharedSecret(serverDHPriv, userDHPub)
	if err != nil {
		return fmt.Errorf("HandleServerKeyExchange: compute shared secret: %w", err)
	}

	// 8. Derive directional keys.
	clientWriteKey, serverWriteKey, err := DeriveWriteKeys(
		sharedSecret,
		userDHPub,
		serverDHPub,
	)
	if err != nil {
		return fmt.Errorf("HandleServerKeyExchange: derive write keys: %w", err)
	}

	// 9. Server writes with serverWriteKey, reads with clientWriteKey.
	conn.writeKey = serverWriteKey
	conn.readKey = clientWriteKey
	conn.writeSeq = 0
	conn.readSeq = 0

	return nil
}