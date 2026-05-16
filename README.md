# `mininet`

## Introduction

`mininet` is a from-scratch virtual network stack, written in Go, that runs entirely
on one machine. It includes:

1. An IP layer, largely based on RFC 791, with virtual interfaces, packet forwarding, 
   longest-prefix match, and RIP (version 2, as specified in RFC 2453);
2. A TCP layer, which implements most of RFC 9293, with sockets, connection 
   setup/teardown, retransmissions, flow control, and zero-window probing; and
3. A TLS layer, which uses authenticated Diffie-Hellman key exchange and 
   sends encrypted, authenticated cipherblocks over our TCP stack.

The project runs every virtual host/router node as a local process. Each node has a 
virtual IP address with virtual interfaces, each interface binds a localhost UDP 
socket, and each `.lnx` config file tells a node which neighbor virtual IPs 
correspond to which localhost UDP addresses. UDP is only the local transport we use 
to move raw packet bytes between adjacent virtual interfaces, representing the low
level 'link layer' in the OSI model - the IP, TCP, and TLS logic above that is ours. 
This project was built as part of coursework and final project for CS1680 at Brown.

## Table of Contents

[Project Structure](#project-structure) - package setup and binaries  
[Demo](#demo) - TLS demo over the full stack  
[Virtual Network Configuration](#virtual-network-configuration) - host and router configuration on one device  
[Quick Start](#quick-start) - build and run the example network  
[IP Layer](#ip-layer) - technical notes on IP: forwarding, route tables, and RIP  
[TCP Layer](#tcp-layer) - technical notes on TCP: sockets, reliable streams, and flow control  
[TLS Layer](#tls-layer) - technical notes on TLS: authenticated key exchange and encrypted records  
[Full mininet Workflow](#full-mininet-workflow) - full workflow of a message through the stack  
[Generating and Running Networks](#generating-and-running-networks) - topology generation and run targets  
[Additional Notes](#additional-notes) - links to the layer-specific READMEs

## Project Structure

```text
cmd/
  vhost/main.go           host entrypoint: IP + TCP + TLS
  vrouter/main.go         router entrypoint: IP + RIP

pkg/
  linkLayer/              UDP-backed virtual links
  ipStack/                IP forwarding, packet handling, RIP, REPL commands
  tcpStack/               sockets API, TCP state machine, buffers, retransmits
  tlsStack/               authenticated key exchange and record encryption
  lnxconfig/              .lnx config parsing
  protocol/               shared IP/TCP utility functions

nets/                     source topology JSON files
generatedNets/            generated per-node .lnx config directories
util/                     topology generator and tmux runner
```

There are two binaries, `vhost` and `vrouter`:

- `vhost` runs a virtual host. It initializes IP, registers protocol `0` for test packets, registers protocol `6` for TCP, starts the TCP stack, starts the TLS stack, and exposes one shared REPL.
- `vrouter` runs a virtual router. It initializes IP, registers protocol `200` for RIP, starts forwarding, and runs the RIP update/timeout loops when configured.

## Demo

The video below is a demo of the TLS layer, which sits atop the TCP and IP layers.
Thus this demo also implicitly shows the functionality of the rest of the project.

https://github.com/user-attachments/assets/99d6d087-dff7-4f38-9d10-22df5e9bf0ab

## Virtual Network Configuration

The source topology files in `nets/` describe nodes and links. For example,
`nets/linear-r1h2.json` describes one router and two hosts connected by two
networks. The generator turns that topology into `.lnx` files with concrete
interface addresses, UDP bind addresses, neighbors, routes, and timing constants.

An excerpt from `generatedNets/linear-r1h2/h1.lnx`:

```text
interface if0 10.0.0.1/24 127.0.0.1:5000 # to network r1-left
neighbor 10.0.0.2 at 127.0.0.1:5001 via if0 # r1

routing static
route 0.0.0.0/0 via 10.0.0.2
```

Which corresponds to: 

| Config line | Interpretation |
|---|---|
| `interface if0 10.0.0.1/24 127.0.0.1:5000` | Create virtual interface `if0`, assign it VIP `10.0.0.1/24`, and bind its local link-layer UDP socket to `127.0.0.1:5000`. |
| `neighbor 10.0.0.2 at 127.0.0.1:5001 via if0` | The directly connected virtual neighbor `10.0.0.2` can be reached by sending bytes to UDP address `127.0.0.1:5001` on `if0`. |
| `route 0.0.0.0/0 via 10.0.0.2` | Any non-local destination should be forwarded to the router. |

The router has two interfaces and one neighbor on each side:

```text
interface if0 10.0.0.2/24 127.0.0.1:5001 # to network r1-left
neighbor 10.0.0.1 at 127.0.0.1:5000 via if0 # h1

interface if1 10.1.0.1/24 127.0.0.1:5002 # to network r1-right
neighbor 10.1.0.2 at 127.0.0.1:5003 via if1 # h2

routing rip
```

So, the "virtual network on one computer" is not simulated by changing the OS
routing table, but rather by process-local state, where each node parses its own
config, opens its own UDP sockets, and implements its own forwarding behavior.

## Quick Start

### Build the host and router binaries:

```sh
make clean all
```

### Generate a net from a `.lnx` config file using the following Makefile rule:

```sh
make net NET=<net_name>
```
where `net_name` is one of the files in the `nets/` directory. For example:
```sh
make net NET=linear-r1h2
```

### Run the generated network:

```sh
make l-r1h2
```

This runs the `linear-r1h2` network, which has the following topology:

```
h1 -- r1 -- h2
```

`h1` has virtual IP `10.0.0.1`, `r1` has `10.0.0.2` and `10.1.0.1`, and `h2`
has `10.1.0.2`. A packet from `h1` to `h2` is sent as bytes from `h1`'s UDP
socket to `r1`'s UDP socket, processed by `r1`'s IP layer, then sent from `r1`'s
other UDP socket to `h2`. From the IP stack's point of view, those UDP sockets
are just links between directly connected virtual neighbors.

This opens a `tmux` session with panes for `h1`, `r1`, and `h2`.

Generated addresses for `linear-r1h2`:

| Node | Interface | Virtual IP | Local UDP address | Neighbor |
|---|---|---:|---:|---|
| `h1` | `if0` | `10.0.0.1` | `127.0.0.1:5000` | `r1` at `10.0.0.2` |
| `r1` | `if0` | `10.0.0.2` | `127.0.0.1:5001` | `h1` at `10.0.0.1` |
| `r1` | `if1` | `10.1.0.1` | `127.0.0.1:5002` | `h2` at `10.1.0.2` |
| `h2` | `if0` | `10.1.0.2` | `127.0.0.1:5003` | `r1` at `10.1.0.1` |

In any node pane, type `q` to quit that node.


## IP Layer

The IP layer owns interfaces, neighbors, forwarding, packet validation, and RIP.
It is shared by both hosts and routers; routers simply enable RIP and use the
same forwarding table with additional route metadata.

Relevant files:

```text
pkg/ipStack/
  ipStack.go          initialization, forwarding, LPM, marshalling
  replAndPrint.go     IP REPL commands and table printing
  rip.go              RIP updates, triggered updates, route timeouts
  types.go            IP stack, forwarding table, and RIP structs

pkg/linkLayer/
  linkLayer.go        UDP read/write wrappers for virtual links
  types.go            Interface and Neighbor types
```

The top-level IP stack is:

```go
type IPStack struct {
    Interfaces         map[string]*ll.Interface
    ForwardingTable    map[netip.Prefix]*FwdEntry
    mu                 sync.Mutex
    IncomingPacketChan chan []byte
    recvHandlers       map[int]ReceiveHandler
    RipInfo            RipInfo
}
```

The forwarding table stores local routes, static routes, and RIP-learned routes
in one structure. Router entries also track cost, route type, and last update
time, which lets the same table support normal forwarding, periodic RIP updates,
triggered updates, and route expiration.

### IP Packet Processing

When bytes arrive on an interface's UDP socket, the link layer pushes them into
`IncomingPacketChan`. `RunIPLayer()` parses and validates the IPv4 header, checks
whether the packet is destined for a local interface, and either dispatches it to
the registered protocol handler or forwards it. Forwarding means decrementing
TTL, running longest-prefix match, resolving the next hop to a directly connected
neighbor, and sending the packet out through the appropriate virtual interface.

For a plain test packet from `h1` to `h2`:

```text
h1> send 10.1.0.2 hello-from-h1
```

`h2` receives:

```text
[HandleTestMessage] Received message from 10.0.0.1: hello-from-h1
```

IP REPL commands:

| Command | Function |
|---|---|
| `li` | List interfaces. |
| `ln` | List directly connected neighbors. |
| `lr` | List forwarding table entries. |
| `send <ip> <message>` | Send a test packet using IP protocol `0`. |
| `down <ifname>` | Mark an interface down. |
| `up <ifname>` | Mark an interface up. |

## TCP Layer

The TCP layer runs only on hosts. It registers a handler for IP protocol `6` and
exposes a socket-like API to the REPL and to TLS:

```go
VListen(port)
VAccept()
VConnect(addr, port)
VRead(buf)
VWrite(data)
VClose()
```

Relevant files:

```text
pkg/tcpStack/
  types.go          structs, constants, connection states
  socketsAPI.go     VListen, VAccept, VConnect, VRead, VWrite, VClose
  handshake.go      SYN / SYN-ACK / ACK connection setup
  doTcp.go          packet handling, send loop, ACKs, payloads, RTO, ZWP
  closing.go        FIN handling, TIME_WAIT, teardown
  circleBuf.go      circular send/receive buffer
  earlyArrivals.go  min-heap for out-of-order segments
  tcpRepl.go        TCP REPL commands
```

The top-level TCP stack is:

```go
type TCPStack struct {
    socketTable  *SocketTable
    ipStack      *ipStack.IPStack
    sendRequests chan *SendRequest
}
```

All connections send outgoing packets through `sendRequests`. One goroutine
drains that channel and hands serialized TCP packets to IP, which keeps IP access
centralized even though each connection has its own send loop.

The implementation covers the core mechanics of TCP as specified in RFC 9293: 
handshakes and teardown are stateful, `VWrite`/`VRead` use send and receive 
buffers, ACKs drive retransmission queue cleanup, out-of-order segments wait in 
an early-arrivals heap, and zero-window probing keeps a sender from getting 
permanently stuck when the receiver advertises no space.

### Example

In `h2`, listen:

```text
a 8080
```

In `h1`, connect:

```text
c 10.1.0.2 8080
```

The client creates a socket in `SYN_SENT`, sends a SYN through IP, waits on its
`establishedChan`, and returns after the handshake reaches `ESTABLISHED`. The
listener creates accepted sockets as SYNs arrive and places them on its accept
channel.

Send data from `h1`:

```text
s <h1-socketID> hello-over-tcp
```

Read on `h2`:

```text
r <h2-socketID> 14
```

TCP REPL commands:

| Command | Function |
|---|---|
| `a <port>` | Listen and accept TCP connections. |
| `c <vip> <port>` | Connect to a listener. |
| `ls` | List TCP sockets and states. |
| `s <socketID> <bytes>` | Send bytes. |
| `r <socketID> <numBytes>` | Read bytes. |
| `sf <path> <vip> <port>` | Send a file over TCP. |
| `rf <destPath> <port>` | Receive a file over TCP. |
| `cl <socketID>` | Close a socket. |
| `rst <socketID>` | Send a reset and tear down locally. |

## TLS Layer

A lightweight version of TLS wraps our TCP API. It does not implement full TLS or 
certificates, but it does implement the central security structure: authenticated 
ephemeral key exchange followed by encrypted, authenticated records, achieving
message integrity, confidentiality, and authentication.

Relevant files:

```text
pkg/tlsStack/
  tlsApi.go       VTLSListen, VTLSAccept, VTLSDial, VTLSRead, VTLSWrite
  handshake.go    authenticated X25519 key exchange
  crypto.go       X25519, Ed25519, HKDF, AES-GCM helpers
  messages.go     serializable handshake messages
  tlsRepl.go      TLS REPL commands
  types.go        TLS connection, listener, config, and message types
```

The public TLS API mirrors TCP:

```go
VTLSListen(port, serverConfig)
VTLSAccept()
VTLSDial(addr, port, clientConfig)
VTLSRead(buf)
VTLSWrite(data)
VTLSClose()
```

### Key Exchange

The handshake uses ephemeral X25519 Diffie-Hellman values and pre-shared Ed25519
signing keys. The signing keys stand in for a certificate/public-key trust
system, which is out of scope for this project.

Broadly, the client and server exchange ephemeral X25519 public values, use those
values to compute the same shared secret, and derive symmetric write keys from
that secret. The wrinkle that makes the exchange authenticated is that each side
signs the DH values with its Ed25519 signing key, and the peer verifies that
signature using a trusted public key. That prevents a middlebox from swapping in
its own DH value without being detected.

The directional key convention is:

| Key | For |
|---|---|
| `clientWriteKey` | Client encrypts, server decrypts. |
| `serverWriteKey` | Server encrypts, client decrypts. |

### Record Layer

After the handshake, TLS behaves like a small record layer. `VTLSWrite`
turns plaintext into an AES-GCM encrypted/authenticated record, prefixes it with
its length, and writes it through TCP. `VTLSRead` reads a full record from TCP,
checks the authentication tag, decrypts it, and returns plaintext to the caller.

### Example

In `h2`, listen:

```text
tlsa 4443
```

In `h1`, connect:

```text
tlsc 10.1.0.2 4443
```

Both sides print handshake debug output with short hashes of their read/write
keys. The hashes should be crossed: the client's write key should match the
server's read key, and the server's write key should match the client's read key.

Send from `h1`:

```text
tlss <h1-tlsID> hello-over-tls
```

Read on `h2`:

```text
tlsr <h2-tlsID> 18
```

TLS REPL commands:

| Command | Function |
|---|---|
| `tlsa <port>` | Listen and accept TLS connections. |
| `tlsc <vip> <port>` | Connect to a TLS listener. |
| `tlsls` | List TLS connections. |
| `tlss <connID> <message>` | Send an encrypted message. |
| `tlsr <connID> <numBytes>` | Read decrypted bytes. |
| `tlssf <path> <vip> <port>` | Send a file through TLS. |
| `tlsrf <destPath> <port>` | Receive a file through TLS. |
| `tlscl <connID>` | Close a TLS connection. |

## Full mininet Workflow

For an encrypted message from `h1` to `h2`, the TLS layer first encrypts the 
plaintext and writes a length-prefixed record to TCP. TCP treats those bytes as 
stream data, sending TCP packets through IP protocol `6`. IP forwards those 
packets through `r1` using the forwarding table, and `h2` dispatches the delivered 
protocol `6` payloads back to TCP. Once TCP has reassembled the stream, TLS reads 
a full record, authenticates and decrypts it, and returns plaintext to the REPL.

That layering is also how the code is organized. TLS depends on TCP's public
API. TCP depends on IP send/receive handlers. IP depends on the UDP-backed link
layer to move bytes only to directly connected virtual neighbors.

## Generating and Running Networks

To regenerate the `linear-r1h2` network in `generatedNets/`:

```sh
util/vnet_generate nets/linear-r1h2.json generatedNets/linear-r1h2
```
or use the `net` target in the Makefile:

```sh
make net NET=<net_name>
```

Then run it:

```sh
make l-r1h2
```

or

```sh
util/vnet_run ./generatedNets/linear-r1h2
```

Nota bene there are several built-in network targets, includign:

```sh
make l-r1h4     # 1 router with 4 hosts
make l-r3h2     # 2 hosts separated by 3 routers
make lp         # 2 hosts connected in a loop, the first path with 3 routers, the second with 2 routers
make dabc       # 1 host connected to a loop of 3 routers at one point
```

## Additional Notes

The layer-specific README writeups, located in `/spec`, contain more implementation detail:

- `README-IP.md` details the IP stack: forwarding table, RIP, link-layer structs, and goroutines;
- `README-TCP.md` covers the TCP layer: TCP data structures, channels, zero-window probing, retransmissions, and early arrivals; and
- `README-TLS.md` covers the TLS layer: TLS design, authenticated key exchange, record layer, and cryptographic primitives.
