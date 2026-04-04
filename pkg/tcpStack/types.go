package tcpstack

import (
	"ip-isabelle-and-ben/pkg/ipStack"
	"net/netip"
	"sync"
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

	/* store a socket (either normal or listener) */
	normalSocket	*VTCPConn
	listenSocket	*VTCPListener

	/* for telling initial sender that connection has been established
		after 3-way handshake */
	establishedChan chan int
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

/* tragic but yeah gotta have this too */
type TCPStack struct {
	socketTable 	*SocketTable
	ipStack			*ipStack.IPStack
}

/* listener socket object */
type VTCPListener struct {
	port 			uint16
	connChan 		chan *VTCPConn
	acceptingConns 	bool /* true if Accept() has been called--idk if there's a better way to do this */
}

/* actual "normal socket" object */
type VTCPConn struct {
	packetChan chan []byte

	// send buffer
	// receive buffer
	// channel from VWrite to send buffer thread
	// channel from handleTCP to receive buffer thread
	// channel from recieve buffer thread to VRead

	// 	- retransmission queue (later) (could also go in Conn) (will require third thread)

}

/* send buffer struct
	- actual buffer

	- pointers into buffer
	
	- our available window size
	- 
*/

/* receive buffer struct
	- actual buffer
	- min heap for early arrivals
	- pointers into buffer

*/



