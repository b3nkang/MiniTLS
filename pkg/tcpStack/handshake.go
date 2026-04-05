package tcpstack

import (
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"

	"github.com/google/netstack/tcpip/header"
)

/* send initial SYN */
func (tcp *TCPStack) sendSyn(tableEntry *SocketTableEntry) error {
	fmt.Println("[TCP] entering send SYN function")
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       tableEntry.localPort,
		DstPort:       tableEntry.destPort,
		SeqNum:        tableEntry.seqNum,
		DataOffset:    20, 			/* TODO: I have no idea what this is */
		Flags:         header.TCPFlagSyn,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}

	/* send using sendTCP */
	tcp.sendTCP(tcpHdr, tableEntry.localIP, tableEntry.destIP, make([]byte, 0))

	/* print for now */
	fmt.Println("[TCP] printing socket table for now")
	tcp.socketTable.listSockets()
	return nil
}

/* What we do when we get a match on a listen socket
(if listen socket is accepting)
	1. Make normal conn
	2. Add to socket table with SYN_RECEIVED state
	3. send SYN-ACK
*/
func (tcp *TCPStack) handleSyn(listener *VTCPListener, tcpHeader header.TCPFields, localIP netip.Addr, destIP netip.Addr) {
	table := tcp.socketTable

	/* check if Accept() has been called on listener */
	if !listener.acceptingConns {
		fmt.Println("[TCP] tried to connect to a listen socket that is not accepting conns--dropping packet")
		return
	}

	/* ensure packet is actually SYN */
	if (tcpHeader.Flags & header.TCPFlagSyn) == 0 {
		/* if SYN flag is not set, drop packet */
		fmt.Println("[TCP] TCP packet sent to listener that does not have SYN flag set--dropping packet")
		return
	}

	/* lock table mutex since we are modifying it */
	table.mu.Lock()

	/* make normal conn */
	conn := &VTCPConn{
		packetChan: make(chan []byte),
	}
	fmt.Println("[TCP] normal socket created from listen socket--adding to socket table")
	/* make socket table entry and add in correct state */
	entry := &SocketTableEntry{
		localPort: tcpHeader.DstPort,
		localIP: localIP,
		destPort: tcpHeader.SrcPort,
		destIP: destIP,
		state: SYN_RECEIVED,
		normalSocket: conn,
		listenSocket: listener,
		socketID: table.nextID,
		seqNum: utils.GenerateNewSeq(), /* generate random sequence number here for starting */
	}

	table.nextID++ /* need to increment for next connection */
	table.socketMap[entry.socketID] = entry

	table.mu.Unlock() /* UNLOCK MUTEX BEFORE SENDING SYNACK */
	tcp.sendSynAck(entry, tcpHeader.SeqNum)
}


/* send SYN-ACK (second part of 3-way handshake) */
func (tcp *TCPStack) sendSynAck(tableEntry *SocketTableEntry, synSeq uint32) error {
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       tableEntry.localPort, 	// TODO: from Ben - it is too early in the morning for me to think so 
		DstPort:       tableEntry.destPort,		// 		 i may be tripping but this looks sus to me, do we need to flip these
		SeqNum:        tableEntry.seqNum,		//	     i will revisit this some other time when i am clear minded
		AckNum:        synSeq+1, 	/* Ack should be whatever we got from SYN + 1 */
		DataOffset:    20, 			
		Flags:         header.TCPFlagSyn | header.TCPFlagAck,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,	
	}							// *********** TODO: later: get some nice table prints of the socketmap so we can verify stuff

	/* send using sendTCP */
	tcp.sendTCP(tcpHdr, tableEntry.localIP, tableEntry.destIP, make([]byte, 0))
	return nil

}

/* handle incoming SYN-ACK and send ACK */
func (tcp *TCPStack) handleSynAck(tableEntry *SocketTableEntry, tcpHeader header.TCPFields) error {
	synAck := uint8(header.TCPFlagSyn) | uint8(header.TCPFlagAck)
	if (tcpHeader.Flags & synAck) != synAck {
		// not a SYN-ACK, drop	
		tableEntry.establishedChan <- ERROR // unblock vconnect
		return fmt.Errorf("flags for handleSynAck do not match expected SYN | ACK")
	}

	/* lock table mutex since we are modifying it */
	table := tcp.socketTable
	table.mu.Lock()

	// verify ack has been updated correctly
	if tcpHeader.AckNum != tableEntry.seqNum+1 {
		tableEntry.establishedChan <- ERROR // unblock vconnect
		return fmt.Errorf("AckNum does not match SeqNum; %d != %d", tcpHeader.AckNum, tableEntry.seqNum+1)
	}

	tableEntry.seqNum = tcpHeader.AckNum
	tableEntry.state = ESTABLISHED
	table.mu.Unlock() /* UNLOCK MUTEX BEFORE CALLING SENDACK */
	tcp.sendAckHandshake(tableEntry, tcpHeader.SeqNum)
	/* set up send and recv buffers for comms */
	tableEntry.normalSocket.initBufs()
	
	tableEntry.establishedChan <- tableEntry.state // unblock vconnnect
	return nil
}

func (tcp *TCPStack) sendAckHandshake(tableEntry *SocketTableEntry, passiveSeqNum uint32) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       tableEntry.localPort, // TODO: dir should be right but verify some other time
		DstPort:       tableEntry.destPort,
		SeqNum:        tableEntry.seqNum, // already ++ in handle
		AckNum:        passiveSeqNum+1, 	/* Ack should be whatever we got from SYN + 1 */
		DataOffset:    20, 			/* TODO: same as other instances */
		Flags:         header.TCPFlagAck,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}

	/* send using sendTCP */
	tcp.sendTCP(tcpHdr, tableEntry.localIP, tableEntry.destIP, make([]byte, 0))
	return nil
}

/* last step for passive side of connection (VAccept caller) -> init bufs after this */
func (tcp *TCPStack) handleAckHandshake(tableEntry *SocketTableEntry, tcpHeader header.TCPFields) error {
	if (tcpHeader.Flags & header.TCPFlagAck) == 0 {
		// not an ACK, drop	
		return nil
	}

	// lock
	table := tcp.socketTable
	table.mu.Lock()

	// verify ack has been updated correctly
	if tcpHeader.AckNum != tableEntry.seqNum+1 {
		return fmt.Errorf("AckNum does not match SeqNum; %d != %d", tcpHeader.AckNum, tableEntry.seqNum+1)
	}
	// if so, update passive side's seqNum in tableentry to be consistent
	tableEntry.seqNum += 1

	// return state
	tableEntry.state = ESTABLISHED

	/* init send/recv bufs before returning */
	normalSock := tableEntry.normalSocket
	normalSock.initBufs()
	// TODO: all pointer logic below is naive and may need to be revised
	normalSock.recvBuf.lbr = tableEntry.seqNum % MAX_WIN_SIZE 
	normalSock.recvBuf.nxt = normalSock.recvBuf.lbr + 1 
	if normalSock.recvBuf.lbr == MAX_WIN_SIZE-1 {
		normalSock.recvBuf.nxt = 0
	}
	// TODO: init min heap here for early arrivals

	table.mu.Unlock() /* UNLOCK MUTEX BEFORE PASSING SOCKET */
	tableEntry.listenSocket.connChan <- tableEntry.normalSocket // unblock vconnnect

	fmt.Println("[TCP] Switching state to ESTABLISHED, unblocking Accept.")
	table.listSockets()
	fmt.Println(">")
	return nil
}
