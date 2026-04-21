# TCP

## Project Structure

```
pkg/tcpStack/
├── types.go          # all types, constants, and struct definitions
├── socketsAPI.go     # public socket API: VListen, VAccept, VConnect, VRead, VWrite, VClose
├── doTcp.go          # core TCP logic: HandleTCP, sendLoop, handlePayload, handlePureAck, ZWP, retransmissions
├── handshake.go      # 3-way handshake: sendSyn, handleSyn, sendSynAck, handleSynAck, handleAckHandshake
├── closing.go        # connection teardown: sendFin, handleFin, handleEarlyFin, timeWait, teardown
├── earlyArrivals.go  # min-heap for out-of-order segments
├── circleBuf.go      # circular buffer used by both send and receive buffers
└── tcpRepl.go        # REPL commands for TCP (ls, c, s, a, sf, rf, sd, etc.)
```

## Data Structures

### TCPStack

Our top-level struct, one per host:

```go
type TCPStack struct {
    socketTable  *SocketTable
    ipStack      *ipStack.IPStack
    sendRequests chan *SendRequest
}
```

* **`socketTable`**: our host's table of all open sockets
* **`ipStack`**: reference to the underlying IP stack we use for sending packets
* **`sendRequests`**: a channel through which all connections send their outgoing packets; one goroutine monitors this channel and handles all packet sends, so only one routine ever touches the IP stack

### SocketTable

```go
type SocketTable struct {
    socketMap  map[int]*SocketTableEntry
    nextID     int
    mu         sync.Mutex
}
```

* **`socketMap`**: maps integer socket ID to its table entry
* **`mu`**: we use this to protect concurrent reads/writes during handshakes and teardown

### SocketTableEntry

One entry per open socket (listener or normal connection):

```go
type SocketTableEntry struct {
    localPort, destPort  uint16
    localIP, destIP      netip.Addr
    state                int           // LISTEN, SYN_SENT, ESTABLISHED, etc.
    socketID             int
    seqNum               uint32
    normalSocket         *VTCPConn
    listenSocket         *VTCPListener
    establishedChan      chan int       // unblocks VConnect after handshake
    sendPacketFunc       func(*SendRequest)
    removeSelf           func(int)
}
```

### VTCPConn

The "normal socket" object we hand to the application:

```go
type VTCPConn struct {
    sendBuf      *SendBuf
    recvBuf      *RecvBuf
    retransQueue *RetransmissionQueue
    socketEntry  *SocketTableEntry
}
```
* **`socketEntry`**: we keep a pointer back to the entry so the connection knows its own state, which is necessary for closing

### SendBuf and RecvBuf

Both buffers wrap a `CircleBuf` (a struct with methods we implemented) and track buffer pointers:

```go
type SendBuf struct {
    cBuf               *CircleBuf
    mu                 sync.Mutex
    base, nxt, lbw, una uint32
    otherSideWindow    uint16
    dataWrittenToBuf   chan struct{}   // VWrite → sendLoop notification
    spaceAvailable     chan struct{}   // sendLoop → VWrite backpressure
    isProbing          bool
    otherSideWindowUpdated chan struct{}
    zwpTrigger         chan struct{}
}
```
* **`base`**: sequence number corresponding to `cBuf.head` (index 0); advances as ACKs come in
* **`nxt`**: next sequence number to send; bytes in `[una, nxt)` are in-flight
* **`lbw`**: last byte written by the application
* **`una`**: earliest sent but un-ACKed sequence number
* **`dataWrittenToBuf`** (chan struct{}): signals the send loop that new data is ready to send
* **`spaceAvailable`** (chan struct{}): signals a blocked write that buffer space was freed
* **`zwpTrigger`** (chan struct{}): kicks off ZWP when the window drops to 0 with unsent data
* **`otherSideWindowUpdated`** (chan struct{}): wakes the ZWP probe loop when a new window advertisement arrives

```go
type RecvBuf struct {
    cBuf          *CircleBuf
    mu            sync.Mutex
    lbr, nxt      uint32
    earlyArrivals *EarlyArrivals     // min-heap for out-of-order segments
    dataToRead    chan struct{}       // handlePayload → VRead notification
    fin           uint32             // seq num of received FIN, or 0
}
```
* **`lbr`**: last byte read by the application; `[lbr+1, nxt-1]` is data buffered and waiting to be read
* **`nxt`**: next sequence number expected from the sender
* **`earlyArrivals`**: our min-heap of out-of-order segments received before the gap is filled
* **`dataToRead`** (chan struct{}): signals a blocked read that new in-order data has been written to the buffer
* **`fin`**: sequence number of a received FIN segment, or 0 if no FIN has been received yet

### RetransmissionQueue

```go
type RetransmissionQueue struct {
    mu    sync.Mutex
    array []*RetransmissionEntry
    rto   time.Duration
    srtt  time.Duration
    timer *time.Timer
}

type RetransmissionEntry struct {
    seqNum        uint32
    len           uint32
    flags         uint8
    sent          time.Time
    retransmitted bool       // Karn's algorithm: skip RTT sample if retransmitted
    numRetransmits int       // for timeouts
}
```

Our queue is a simple ordered slice acting as a FIFO. The head is always the earliest un-ACKed segment, which we retransmit when the RTO timer fires.

### Early Arrivals

A min-heap (Go `container/heap`) of out-of-order segments, ordered by sequence number:

```go
type EarlyArrival struct {
    startSeq uint32
    endSeq   uint32
    data     []byte
}
type EarlyArrivals []*EarlyArrival
```

---

## Concurrency: Goroutines, Channels, and Mutexes

### Goroutines and Timers

| Goroutine | Purpose |
|---|---|
| `tcp.sendPacketsOut()` | Drains `TCPStack.sendRequests` channel; serializes all outgoing packet sends |
| `entry.sendLoop()`  | Per-connection loop that reads from `sendBuf` and sends data segments |
| `time.AfterFunc(rto, retransmitSegment)` | Fires after RTO expires; re-arms itself on each retransmission |
| `time.AfterFunc(2*MSL, teardown)` | Fires teardown after TIME_WAIT period |

### Notable Channels

| Channel | Purpose |
|---|---|
| `TCPStack.sendRequests` | We funnel all `SendRequest` objects from all connections here |
| `SendBuf.dataWrittenToBuf` | Signals that new data was written to the send buffer |
| `SendBuf.spaceAvailable` | Unblocks a write that was waiting for send buffer space |
| `SendBuf.zwpTrigger` |  Triggers the start of Zero Window Probing |
| `SendBuf.otherSideWindowUpdated` | Wakes the send loop when a new window size arrives during ZWP |
| `RecvBuf.dataToRead` | Notifies a blocked read that data is ready |

## Zero Window Probing

When the receive window drops to zero and there is unsent data with nothing already in flight, our pure ACK handler:

1. Sets `sendBuf.isProbing = true`
2. Sends a signal on `sendBuf.zwpTrigger` to wake the send loop

Inside the send loop, when `windowRemaining <= 0` and ZWP conditions are met (window is zero, unsent data exists, nothing in flight), we:

1. Read exactly one byte from the send buffer at `sendBuf.una` (the probe byte) and send it as a normal segment
2. Start (or reset) a `probeTimer` set to `PROBE_ITV` (40ms for automated testing, 4s for manual testing)
3. Wait for either:
   * `sendBuf.otherSideWindowUpdated` — an ACK has arrived with a non-zero window
   * `probeTimer.C` — no response yet, continue looping

When the receiver ACKs the probe byte, our pure ACK handler detects the special case where `isProbing` is set and the ACK number equals the next expected byte: we advance `una`, `nxt`, and the circular buffer base by 1, then signal `otherSideWindowUpdated`. If the new window is still zero, the send loop will probe again; if it's non-zero, `isProbing` is cleared and normal sending resumes.

---

## Retransmissions

Every data segment (and FIN) we send is recorded as a `RetransmissionEntry` appended to `RetransmissionQueue.array`. The entry stores the segment's starting sequence number, length, flags, send time, and a `retransmitted` flag for Karn's algorithm.

**RTO Timer** (`startRtoTimer` / `retransmitSegment`):

* Each time a data segment is sent and the queue was previously empty, we start the RTO countdown via `time.AfterFunc`.
* When an ACK is received, the timer is stopped. If there are still entries in flight, it is restarted (RFC 6298 §5.3).
* When the timer fires, we retransmit the earliest un-ACKed segment, double the RTO (up to `RTO_MAX = 5s`), and restart the timer.
* After `MAX_RETRANSMISSIONS` (4) retransmit attempts, we close and tear down the connection.

**RTO Update** (Karn's Algorithm + SRTT):

* When an ACK dequeues a segment that wasn't retransmitted, we compute a new SRTT and RTO:
  * `SRTT = α * SRTTLast + (1 - α) * RTTMeasured` where `α = 0.85`
  * `RTO = max(RTO_MIN, min(β * SRTT, RTO_MAX))` where `β = 1.65`
* We skip retransmitted segments for RTT measurement (Karn's algorithm) to prevent ambiguity about which transmission was ACKed.




## Early Arrivals

If a segment arrives with a sequence number ahead of what we're expecting, we handle it as follows:

1. Our payload handler detects the early arrival and calls `recvBuf.earlyArrivals.PushSegment(seqNum, data)`
2. We send a pure ACK for the next expected byte back (to prompt the sender to re-send the missing segment)
3. The segment sits in the min-heap, ordered by `startSeq`, until the gap is filled

When the expected in-order segment finally arrives and is written to the recv buffer, our payload handler runs a drain loop:

```
for {
    min := earlyArrivals.Peek()
    if min == nil { break }
    if min.startSeq != recvBuf.nxt { break }
    // space check
    segment := earlyArrivals.PopMin()
    recvBuf.cBuf.WriteIntoBuf(segment.startSeq, segment.data)
    recvBuf.nxt += len(segment.data)
}
```

This drains as many consecutive early arrivals as possible in one shot. After the heap is empty, we check whether a previously stored early FIN can now be processed (i.e., all data before the FIN has arrived).

Our min-heap satisfies Go's `container/heap` interface on `EarlyArrivals`. The `Less` function orders by `startSeq` so that `Peek()` always returns the segment with the smallest (next-expected) sequence number.
