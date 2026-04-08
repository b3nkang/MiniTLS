package tcpstack

import (
	"ip-isabelle-and-ben/pkg/ipStack"
	"net/netip"
	"sync"

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
	ERROR /* just made this for 3-way handshake */
)

const (
	MAX_WIN_SIZE = 65535 /* check this...*/
)

/* info about 1 socket in table */
type SocketTableEntry struct {
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
}

/* actual "normal socket" object */
type VTCPConn struct {
	packetChan chan []byte /* may not need? */
	sendBuf 	*SendBuf
	recvBuf		*RecvBuf

	// send buffer
	// receive buffer
	// channel from VWrite to send buffer thread
	// channel from handleTCP to receive buffer thread
	// channel from recieve buffer thread to VRead

	// 	- retransmission queue (later) (could also go in Conn) (will require third thread)
}

type SendBuf struct {
	buf []byte 		/* simple normal array for now */
	currSize uint32	/* curr amt of data in buf */	

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
}

type RecvBuf struct {
	buf []byte		/* simple normal array for now */
	currSize uint32	/* curr amt of data in buf */

	mu sync.Mutex 	/* mutex between incoming packet logic and vread */
	base uint32 	/* sequence num at index=0*/

	/* pointers */
	lbr uint32		/* last byte read (next byte that gets read when app calls read) */
	nxt uint32		/* next sequence num expected */

	/* channels */
	dataToRead chan struct {}
}



