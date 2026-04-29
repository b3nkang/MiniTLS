# MiniTLS

## Outline

MiniTLS is our plan to build on top of our TCP layer to implement a lightweight version of the TLS protocol, which prevents various attacks from happening to our packets such as eavesdropping, tampering, impersonation, MitM, replay attacks, and more. While the official TLS protocol (RFC 8846\) is extremely extensive and impractical to implement, our approach will focus on establishing reasonably meaningful cryptographic safeguards for our TCP layer, so that messages have a higher level of security associated with them. Concretely, this would mean offering an API suite consisting of something like the following:

```go
func TLSDial(addr netip.Addr, port uint16) (*SomeTLSConn, error)
func TLSListen(port uint16) (*SomeTLSListener, error)
func TLSAccept() (*SomeTLSConn, error)
func TLSWrite(data []byte) (int, error)
func TLSRead(buf []byte) (int, error)
func TLSClose() error
```

Which are mostly just wrappers around our TCP API functions, except with a handshake layer to authenticate and derive shared keys, and another layer to actually convert plaintext to encrypted and signed ciphertext which is sent over TCP and then authenticated at the TLS layer.

## Basic / Target / Stretch Goals

Across all cases, our goal will be to establish a record layer that encrypts and decrypts (and perhaps authenticates) messages above the TCP layer. On send, this means splitting the plaintext, adding the TLS-appropriate header (with some modifications for simplicity), encrypting, then sending the cipher over TCP. On receive, it means reading the header and extracting the cipher, decrypting (and perhaps authenticating), and then returning in plaintext.

In both the basic and target versions of this project, we intend to support only a send/receive interface, as opposed to a ‘sendFile’/‘receiveFile’ as was done in TCP.

### Basic

As for basic goals, the idea would be to have the cryptographic element of TLS we implement go as far as symmetric, unauthenticated Diffie-Hellman Key Exchange (DHKE). This is fairly simple to implement, so the focus would be on getting the encryption/decryption working at the record layer of TLS. However, the downside is that this is still a fairly insecure protocol cryptographically, we do prevent eavesdroppers by establishing confidentiality; however, this protocol still lacks integrity due to MitM attacks and authentication since we have no idea who we are talking to.

### Target

Instead of just blindly trusting who we are talking to, our target version will use authenticated DHKE, which provides integrity and authentication on top of privacy. This will involve signing the DHKE with a secret key, and the receiver correspondingly having access to a trusted public key they can use to verify the message. Note, however, that we will not be implementing certificates, since this is too much work and not worth the time expenditure (hence, MiniTLS), so we just assume that the public key is pre-shared from a trustworthy source.

### Stretch

At this point, we have a number of options on where to extend the project. One idea is to actually get a version of ‘sendFile’/‘receiveFile’ working that uses TLS. Another option is to preserve TLS handshake state from a previous connection and store locally on the machine, so that we don’t need to go through the full TLS handshake process where it is not necessary.

## Tools/Libraries

Our IP and TCP stacks are both written in Go, so the TLS layer will be also. To do so, we will make use of Go’s standard `crypto` library, which has functions that already implement things like DHKE (`crypto/ecdh` with `ecdh.X25519()`), signatures (`crypto/ed25519`), and enc/auth with `crypto/aes` and `crypto/cipher`.

## AI Use

Conceptually, some of the cryptographic elements are quite heavy, so we will rely on AI to guide us on which crypto packages are the best or which functions are the most appropriate to use. One of our team members has implemented many of these crypto utils from scratch before in C++ (e.g. HKDF) and it’s quite menial, so using the right functions will cut out a lot of the work. So a lot of the crypto plumbing we intend to have AI perhaps not build, but point us in the right direction of what to use. Also it will be useful for general cryptographic explanations for why we want to do things a certain way so we don’t miss out on any major cryptographic lapses. 

As for what we will build manually, all TLS-RFC relevant work related to header and encryption setup, payload formatting, handling messages, and having it play nice with things such as MSS and chunking at the TCP layer will be handled manually. In essence, all network-relevant content we will handle ourselves, and AI will help guide our cryptographic understanding.