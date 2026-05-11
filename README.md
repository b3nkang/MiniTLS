# MiniTLS

MiniTLS is a lightweight TLS layer over our custom TCP stack that performs authenticated Diffie-Hellman key exchange, derives shared encryption keys, and sends messages or files as encrypted, authenticated records. Cryptographically, the implementation achieves confidentiality, integrity, peer authentication, and protection against impersonation and man-in-the-middle attacks.

https://github.com/user-attachments/assets/99d6d087-dff7-4f38-9d10-22df5e9bf0ab

## Overview

MiniTLS is our plan to build on top of our TCP layer to implement a lightweight version of the TLS protocol, which prevents various attacks from happening to our packets such as eavesdropping, tampering, impersonation, MitM, replay attacks, and more. While the official TLS protocol (RFC 8846\) is extremely extensive and impractical to implement, our approach will focus on establishing reasonably meaningful cryptographic safeguards for our TCP layer, so that messages have a higher level of security associated with them.

The implementation consists largely just of wrappers around our TCP API functions; concretely, this involves two main cryptographic elemetns: authenticated key exchange and encryption/decryption layer. 

Our version of authenticated key exchange does not involve certificates, which we deem out of scope (since we would need to implement an issuing authority also). Instead, we uses ephemeral X25519 Diffie-Hellman to derive a shared secret, then have each side sign the DH public values with pre-shared Ed25519 signing keys. Then, each side verifies the other’s signature with a trusted public key, preventing impersonation/MitM, and both sides derive directional AES-GCM keys from the shared secret using HKDF.

After the handshake, we implement a small record layer. On write, plaintext is encrypted and authenticated with AES-GCM, length-prefixed, and sent over our existing TCP VWrite path. On read, we reads a full ciphertext from TCP, verify and decrypt, and return plaintext to the TLS layer. Each side uses monotonically increasing sequence numbers to generate unique nonces for AES-GCM ciphers.

Finally, we exposes a TLS-layer API suite similar to the TCP command flow: `tlsa`/`tlsc` establish authenticated encrypted connections (connect/accept), `tlss`/`tlsr` send and read encrypted messages, and `tlssf`/`tlsrf` send and receive whole files by repeatedly calling the TLS read/write layer.

## Tools/Libraries

Our IP and TCP stacks are both written in Go, so the TLS layer is also. We make use of Go’s standard `crypto` library, such as DH utils from `crypto/ecdh`'s `ecdh.X25519()` signatures from `crypto/ed25519`, and enc/auth with `crypto/aes` and `crypto/cipher`.
