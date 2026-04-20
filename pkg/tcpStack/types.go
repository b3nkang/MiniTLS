package tcpstack

import (
	"ip-isabelle-and-ben/pkg/ipStack"
	"net/netip"
	"sync"
	"time"

	"github.com/google/netstack/tcpip/header"
)

/* Represent states
iota = use increasing numbers */
const (
    CLOSED = iota
    LISTEN
    SYN_SENT
    SYN_RECEIVED
    ESTABLISHED
	FIN_WAIT_1
	CLOSE_WAIT
	LAST_ACK
	FIN_WAIT_2
	TIME_WAIT
	ERROR /* just made this for 3-way handshake */
)

const (
	MAX_WIN_SIZE = 10
	MAX_SEG_SIZE = 1  /* 1360 MAX, but we can choose whatever we want */
	MAX_SEGMENT_LATENCY = 3 /* should be 2 minutes, but reducing for testing */
)

// for RTO and SRTT calculations
// initial values taken from slides
const (
	RTO_MIN = 1 * time.Second		// in milliseconds
	RTO_MAX = 5 * time.Second		// in milliseconds
	RTO_INIT = 1 * time.Second		// in milliseconds, 
									// RFC 6298 (2.1): 
									// 		Until a round-trip time (RTT) measurement has been made for a
									//		segment sent between the sender and receiver, the sender SHOULD
									//		set RTO <- 1 second, though the "backing off" on repeated
									// 		retransmission discussed in (5.5) still applies
	RTO_ALPHA = 0.85
	RTO_BETA = 1.65
	PROBE_ITV = 4 * time.Second		// randomly chosen interval at which to send probes
)

/* info about 1 socket in table */
type SocketTableEntry struct {
	dropForRetrans bool
	
	/* 4-tuple stuff */
	localPort 		uint16
	localIP			netip.Addr
	destPort		uint16
	destIP			netip.Addr
	state			int
	socketID		int

	/* header stuff */
	seqNum 			uint32
	lastKnownAck	uint32

	/* store a socket (either normal or listener) */
	normalSocket	*VTCPConn
	listenSocket	*VTCPListener

	/* for telling initial sender that connection has been established
		after 3-way handshake */
	establishedChan chan int

	/* for sending packets without exposing whole tcpStack */
	sendPacketFunc	func(request *SendRequest) /* use to send packets from tcpStack */

	/* removing itself from the scoketTable */
	removeSelf		func(int)
}

/* 1 per host: stores all info about open sockets 
	maps socketID to table entry
	would have to iterate through to check tuple values
	or IDs either way--easier to make map key the ID
*/
type SocketTable struct {
	socketMap		map[int]*SocketTableEntry
	nextID 			int
	mu 				sync.Mutex 	 /* necessary to protect table */
}

type TCPStack struct {
	socketTable 	*SocketTable
	ipStack			*ipStack.IPStack

	sendRequests 	chan *SendRequest /* channel for conns to send packets they want sent out */
}

/* sent from a Conn/tableEntry to the tcp stack to send out packets */
type SendRequest struct {
	tcpHeader 	*header.TCPFields
	data 		[]byte
	sourceIP 	netip.Addr
	destIP 		netip.Addr
}

/* listener socket object */
type VTCPListener struct {
	port 			uint16
	connChan 		chan *VTCPConn
	acceptingConns 	bool /* true if Accept() has been called--idk if there's a better way to do this */

	/* pointer to socket table entry for state */
	socketEntry 	*SocketTableEntry
}

/* actual "normal socket" object */
type VTCPConn struct {
	packetChan chan []byte /* may not need? */
	sendBuf 		*SendBuf
	recvBuf			*RecvBuf
	retransQueue	*RetransmissionQueue
	socketID		int

	/* not sure if this is kosher, but we gotta know our state */
	socketEntry 	*SocketTableEntry
}

type SendBuf struct {
	// buf []byte 		/* simple normal array for now */
	// currSize uint32	/* curr amt of data in buf */	
	cBuf *CircleBuf

	mu sync.Mutex 	/* mutex for buffer */
	base uint32 	/* sequence num at index=0 */

	/* pointers */
	nxt uint32 				/* nxt byte to send */
	lbw uint32 				/* last byte written to buf (from app) */	
	
	// // NOTE: so I tried to make it work without UNA field and we still need a way to track bytes in flight and UNA actually does a better job than some bytesinflight var so gonna roll with UNA for now. TODO: revisit question after mstone2
	una uint32			/* earliest-sent, but still un-ACKed byte */ //maybe use later? Ben: if we're going to enqueue in-flight with some data structure, no need TODO: revisit question after mstone2
	otherSideWindow uint16 	/* the amount which the other's recv buf (NOT OURS, the OTHER party we are connected with) can receive. used in handlePureAck() */
	
	/* channels */
	dataWrittenToBuf chan struct{} 	/* tells thread that VWrite wrote data to buffer, tragically cannot pass straight bytes because VWrite needs to know num bytes written */
	spaceAvailable chan struct{} 	/* tells VWrite that space has been freed in sendBuf if it's full */

	isProbing bool					// indicates if we are in ZWP
	otherSideWindowUpdated chan struct{} 	// channel to unblock sendLoop's ZWP condition that the window has been updated since we sent a probe and to try again
	zwpTrigger chan struct {} 		// channel to trigger ZWP start in handlePureAck. for complicated reasons this must be distinct from otherSideWindowUpdated channel
}

type RecvBuf struct {
	// buf []byte		/* simple normal array for now */
	// currSize uint32	/* curr amt of data in buf */
	cBuf *CircleBuf

	mu sync.Mutex 	/* mutex between incoming packet logic and vread */
	// base uint32 	/* sequence num at index=0*/ --> in circleBuf now

	/* pointers */
	lbr uint32		/* last byte read (next byte that gets read when app calls read) */
	nxt uint32		/* next sequence num expected */

	/* min heap for early arrivals */
	earlyArrivals *EarlyArrivals // TODO: nit but i think this makes more sense at the conn level but thats just nitpick oop design

	/* channels */
	dataToRead chan struct {}

	fin uint32 		/* if FIN has been sent, this will be FIN SEQ, else 0 */
}

/* circular buffer used in send/recv buf */
type CircleBuf struct {
    buf []byte
    maxSize uint32
    currSize uint32
    baseSeq  uint32
	head int
}

// ---------------- EARLY ARRIVALS ---------------
/* obj stored in min heap for early arrivals */
type EarlyArrival struct {
	startSeq uint32
	endSeq   uint32
	data     []byte
}

/* min heap for early arrivals */
type EarlyArrivals []*EarlyArrival


// -------------- RETRANSMISSIONS --------------
type RetransmissionEntry struct {
	seqNum uint32
	len uint32				// length of data segment sent (so we know what slice between nxt-una to send)
	flags uint8				// flags included in segment (assume ACK unless specified)
	sent time.Time
	retransmitted bool		// a flag for if the entry has been retransmitted. if so, we don't use to update RTT (Karn's)
							// RFC 6298: 
							// 		TCP MUST use Karn's algorithm [KP87] for taking RTT samples.  That
							// 		is, RTT samples MUST NOT be made using segments that were
							//		retransmitted (and thus for which it is ambiguous whether the reply
							// 		was for the first instance of the packet or a later instance).
}

type RetransmissionQueue struct {
	mu sync.Mutex 					// since we might be sending + getting ack concurrently
	head uint32 					// TODO: might not be necessary since slice[1:] should be constant...amortized?
	array []*RetransmissionEntry
	rto time.Duration				// all times are in MILLISECONDS
	srtt time.Duration				// used to calculate an updated srtt for each new pureack
	timer *time.Timer				// a countdown for RTO for when we know to retransmit
}


