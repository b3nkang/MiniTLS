package sockets

import "net/netip"


/* info about 1 socket in table */
type SocketTableEntry struct {
	/* 4-tuple stuff */
	srtPort 		int
	srtIP			netip.Addr
	destPort		int
	destIP			netip.Addr
	state			int
	socketID		int

	/* header stuff */
	seqNum 			int

	/* store a socket (either normal or listener) */
	normalSocket	VTCPConn
	listenSocket	VTCPListener


}

/* 1 per host: stores all info about open sockets 
	maps socketID to table entry
	would have to iterate through to check tuple values
	or IDs either way--easier to make map key the ID
*/
type SocketTable struct {
	socketTable		map[int]SocketTableEntry
}

/* listener socket object */
type VTCPListener struct {
	port 		int
	packetChan 	chan []byte
}

/* actual "normal socket" object */
type VTCPConn struct {
	packetChan chan []byte
}



