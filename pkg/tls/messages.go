package tls

import "fmt"

// -------------------------- UserToServer_DHPublicValue_Message ----------------------------

func (m *UserToServer_DHPublicValue_Message) Serialize() ([]byte, error) {
	if len(m.UserDHPublicValue) != X25519PublicKeyLen {
		return nil, fmt.Errorf("UserToServer_DHPublicValue_Message: invalid UserDHPublicValue length: got %d, want %d",
			len(m.UserDHPublicValue), X25519PublicKeyLen)
	}

	out := make([]byte, 1+X25519PublicKeyLen)

	i := 0
	out[i] = MessageType_UserToServer_DHPublicValue_Message
	i++

	copy(out[i:i+X25519PublicKeyLen], m.UserDHPublicValue)

	return out, nil
}

func (m *UserToServer_DHPublicValue_Message) Deserialize(data []byte) error {
	expectedLen := 1 + X25519PublicKeyLen

	if len(data) != expectedLen {
		return fmt.Errorf("UserToServer_DHPublicValue_Message: invalid message length: got %d, want %d",
			len(data), expectedLen)
	}

	if data[0] != MessageType_UserToServer_DHPublicValue_Message {
		return fmt.Errorf("UserToServer_DHPublicValue_Message: wrong message type: got %d, want %d",
			data[0], MessageType_UserToServer_DHPublicValue_Message)
	}

	i := 1
	m.UserDHPublicValue = append([]byte(nil), data[i:i+X25519PublicKeyLen]...)

	return nil
}


// -------------------------- ServerToUser_DHPublicValue_Message ----------------------------

func (m *ServerToUser_DHPublicValue_Message) Serialize() ([]byte, error) {
	if len(m.ServerDHPublicValue) != X25519PublicKeyLen {
		return nil, fmt.Errorf("ServerToUser_DHPublicValue_Message: invalid ServerDHPublicValue length: got %d, want %d",
			len(m.ServerDHPublicValue), X25519PublicKeyLen)
	}

	if len(m.UserDHPublicValue) != X25519PublicKeyLen {
		return nil, fmt.Errorf("ServerToUser_DHPublicValue_Message: invalid UserDHPublicValue length: got %d, want %d",
			len(m.UserDHPublicValue), X25519PublicKeyLen)
	}

	if len(m.ServerSignature) != Ed25519SignatureLen {
		return nil, fmt.Errorf("ServerToUser_DHPublicValue_Message: invalid ServerSignature length: got %d, want %d",
			len(m.ServerSignature), Ed25519SignatureLen)
	}

	expectedLen := 1 + X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen
	out := make([]byte, expectedLen)

	i := 0
	out[i] = MessageType_ServerToUser_DHPublicValue_Message
	i++

	copy(out[i:i+X25519PublicKeyLen], m.ServerDHPublicValue)
	i += X25519PublicKeyLen

	copy(out[i:i+X25519PublicKeyLen], m.UserDHPublicValue)
	i += X25519PublicKeyLen

	copy(out[i:i+Ed25519SignatureLen], m.ServerSignature)

	return out, nil
}

func (m *ServerToUser_DHPublicValue_Message) Deserialize(data []byte) error {
	expectedLen := 1 + X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen

	if len(data) != expectedLen {
		return fmt.Errorf("ServerToUser_DHPublicValue_Message: invalid message length: got %d, want %d",
			len(data), expectedLen)
	}

	if data[0] != MessageType_ServerToUser_DHPublicValue_Message {
		return fmt.Errorf("ServerToUser_DHPublicValue_Message: wrong message type: got %d, want %d",
			data[0], MessageType_ServerToUser_DHPublicValue_Message)
	}

	i := 1

	m.ServerDHPublicValue = append([]byte(nil), data[i:i+X25519PublicKeyLen]...)
	i += X25519PublicKeyLen

	m.UserDHPublicValue = append([]byte(nil), data[i:i+X25519PublicKeyLen]...)
	i += X25519PublicKeyLen

	m.ServerSignature = append([]byte(nil), data[i:i+Ed25519SignatureLen]...)

	return nil
}


// -------------------------- UserToServer_DHSignature_Message ----------------------------

func (m *UserToServer_DHSignature_Message) Serialize() ([]byte, error) {
	if len(m.UserDHPublicValue) != X25519PublicKeyLen {
		return nil, fmt.Errorf("UserToServer_DHSignature_Message: invalid UserDHPublicValue length: got %d, want %d",
			len(m.UserDHPublicValue), X25519PublicKeyLen)
	}

	if len(m.ServerDHPublicValue) != X25519PublicKeyLen {
		return nil, fmt.Errorf("UserToServer_DHSignature_Message: invalid ServerDHPublicValue length: got %d, want %d",
			len(m.ServerDHPublicValue), X25519PublicKeyLen)
	}

	if len(m.UserSignature) != Ed25519SignatureLen {
		return nil, fmt.Errorf("UserToServer_DHSignature_Message: invalid UserSignature length: got %d, want %d",
			len(m.UserSignature), Ed25519SignatureLen)
	}

	expectedLen := 1 + X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen
	out := make([]byte, expectedLen)

	i := 0
	out[i] = MessageType_UserToServer_DHSignature_Message
	i++

	copy(out[i:i+X25519PublicKeyLen], m.UserDHPublicValue)
	i += X25519PublicKeyLen

	copy(out[i:i+X25519PublicKeyLen], m.ServerDHPublicValue)
	i += X25519PublicKeyLen

	copy(out[i:i+Ed25519SignatureLen], m.UserSignature)

	return out, nil
}

func (m *UserToServer_DHSignature_Message) Deserialize(data []byte) error {
	expectedLen := 1 + X25519PublicKeyLen + X25519PublicKeyLen + Ed25519SignatureLen

	if len(data) != expectedLen {
		return fmt.Errorf("UserToServer_DHSignature_Message: invalid message length: got %d, want %d",
			len(data), expectedLen)
	}

	if data[0] != MessageType_UserToServer_DHSignature_Message {
		return fmt.Errorf("UserToServer_DHSignature_Message: wrong message type: got %d, want %d",
			data[0], MessageType_UserToServer_DHSignature_Message)
	}

	i := 1

	m.UserDHPublicValue = append([]byte(nil), data[i:i+X25519PublicKeyLen]...)
	i += X25519PublicKeyLen

	m.ServerDHPublicValue = append([]byte(nil), data[i:i+X25519PublicKeyLen]...)
	i += X25519PublicKeyLen

	m.UserSignature = append([]byte(nil), data[i:i+Ed25519SignatureLen]...)

	return nil
}