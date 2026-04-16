package tcpstack

import "github.com/google/netstack/tcpip/header"

/*
Questions for hours:
- how to deal with retransmissions and early arrivals with FINs
- Do we need to handle every case for every API function?

*/

/*

Cycle from notes:

ACTIVE CLOSE (side that chooses to close first):
	- Send FIN: segment with sequence num X+1 (takes up a sequence num) and FIN flag
		- switch to FIN_WAIT_1 state
	- Receive ACK for FIN: ACK with X+2 means they got it
		- switch to FIN_WAIT_2 state
		- means we can no longer send data, but we can receive data
		- keep receiving data here
	- Receive FIN from other side (takes up a sequence num for them too), this means other side is done sending
		- switch to TIME_WAIT state
		- wait for a set period of time until we delete socket table entry
	- time runs out: CLOSED
		- delete socket table entry and do other cleanup as necessary

PASSIVE CLOSE (side that is closed on):
	- RECEIVE FIN
		- send ACK that we got that FIN (ACKnum is that seqNum + 1)
		- switch to state CLOSE_WAIT
	- Keep sending data if we want until VClose() is called
		- send FIN and switch state to LAST_ACK
		- once we receive the ACK for our fin, close connection (CLOSED)
*/

/* SEND FIN: note: fins SHOULD be retransmitted if not ACKED */
func (entry *SocketTableEntry) sendFin() {
	/* error if we aren't in established state -> this should be checked in close I think -> yes */

	/* increment seqNum because FIN takes up 1 */
	entry.seqNum += 1

	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort,
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		DataOffset:    20, 			
		Flags:         header.TCPFlagFin,
		WindowSize:    entry.normalSocket.recvBuf.getAvailableWindow(),
		Checksum:      0,
		UrgentPointer: 0,
	}	
	
	/* send using sendTCP */
	sendReq := &SendRequest{
		tcpHeader: tcpHdr,
		data: make([]byte, 0),
		sourceIP: entry.localIP,
		destIP: entry.destIP,
	}
	/* enqueues request on tcpStack's request channel -> eventually calls SendTCP
		will this break the retransmission queue???? no data?? idk */
	entry.sendPacketFunc(sendReq)	

	/* switch state to FIN_WAIT_1 */
	entry.state = FIN_WAIT_1
}

/* Q: what if FIN is an early arrival though? -> i.e. what if its 
		need to handle case where it is somehow...can we just put it on the early arrivals 
		queue and then if we pop a FIN, we just don't do anything with data
		(because it isn't there)? idk */
func (entry *SocketTableEntry) handleFin(tcpHdr header.TCPFields) {
	/* 	- RECEIVE FIN
		- send ACK that we got that FIN (ACKnum is that seqNum + 1)
		- switch to state CLOSE_WAIT */
	entry.sendPureAck(tcpHdr.SeqNum)
}

