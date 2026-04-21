package tcpstack

import (
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
	"time"

	"github.com/google/netstack/tcpip/header"
)

/* send initial SYN */
func (entry *SocketTableEntry) sendSyn() error {
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort,
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		DataOffset:    20, 			/* TODO: I have no idea what this is */
		Flags:         header.TCPFlagSyn,
		WindowSize:    MAX_WIN_SIZE,
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
	/* enqueues request on tcpStack's request channel -> eventually calls SendTCP */
	entry.sendPacketFunc(sendReq)	
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
		seqNum: utils.GenerateNewSeq(), /* generate random sequence number here for starting -- PASSIVE SIDE*/
		receivedSynAck: false,
		numSynAckRetransmissions: 0,
	}

	/* set the function here while we have access to tcp stack -- conn will not when it's trying to send */
	entry.sendPacketFunc = func(sendReq *SendRequest) {
		tcp.sendRequests <- sendReq
	}

	/* set function for self-removal from table */
	entry.removeSelf = tcp.socketTable.Remove

	conn.socketEntry = entry

	table.nextID++ /* need to increment for next connection */
	table.socketMap[entry.socketID] = entry

	table.mu.Unlock() /* UNLOCK MUTEX BEFORE SENDING SYNACK */
	entry.sendSynAck(tcpHeader.SeqNum) /* pass in OTHER SIDE'S sequence num */
	entry.startSynAckRetransTimer(tcpHeader.SeqNum) 
}


/* send SYN-ACK (second part of 3-way handshake) */
func (entry *SocketTableEntry) sendSynAck(otherSideSeq uint32) error {
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort, 	
		DstPort:       entry.destPort,		
		SeqNum:        entry.seqNum,		/* our randomly generated sequence num */
		AckNum:        otherSideSeq+1, 		/* Ack should be whatever we got from SYN + 1 */
		DataOffset:    20, 			
		Flags:         header.TCPFlagSyn | header.TCPFlagAck,
		WindowSize:    MAX_WIN_SIZE,
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
	/* enqueues request on tcpStack's request channel -> eventually calls SendTCP */
	entry.sendPacketFunc(sendReq)
	return nil

}

/* handle incoming SYN-ACK and send ACK */
func (tcp *TCPStack) handleSynAck(tableEntry *SocketTableEntry, tcpHeader header.TCPFields) error {
	synAck := uint8(header.TCPFlagSyn) | uint8(header.TCPFlagAck)
	if (tcpHeader.Flags & synAck) != synAck {
		// not a SYN-ACK, drop	
		tableEntry.signalConnectResult(ERROR)
		return fmt.Errorf("flags for handleSynAck do not match expected SYN | ACK")
	}

	/* tell SYN to stop retransmitting */
	tableEntry.handshakeMu.Lock()
	tableEntry.receivedSyn = true
	tableEntry.handshakeMu.Unlock()
	tableEntry.stopHandshakeTimer()

	/* lock table mutex since we are modifying it */
	table := tcp.socketTable
	table.mu.Lock()

	// verify ack has been updated correctly
	if tcpHeader.AckNum != tableEntry.seqNum+1 { /* ackNum should be OUR sequence number + 1*/
		table.mu.Unlock()
		tableEntry.signalConnectResult(ERROR)
		return fmt.Errorf("AckNum does not match SeqNum+1; %d != %d", tcpHeader.AckNum, tableEntry.seqNum+1)
	}

	// update fields
	tableEntry.seqNum = tcpHeader.AckNum /* we know AckNum = seqNum +1, so update here */
	tableEntry.lastKnownAck = tcpHeader.AckNum /* last seq we KNOW was received by receiver */
	tableEntry.state = ESTABLISHED
	
	/* sets our send buffers with this seq num--same one we use for sending Ack AND first
	data because first Ack does not take up a seqNum 
	/* passing in THEIR (sequence num + 1) for recv buf init 
		this is the same number we use for ACK because it is the next sequence number
		we expect, so should also be what we use to initially set recv.NXT */
	// note per RFC - technically bufs are supposed to be init'd much earlier for coverage of weird edge cases (e.g. simulataneous open) that we don't need to worry about
	tableEntry.initBufs(tcpHeader.SeqNum + 1) 

	// handle storing the initial max window we get for the receiver into the new sendBuf
	sendBuf := tableEntry.normalSocket.sendBuf
	sendBuf.mu.Lock() // don't think this is technically needed but why not
	sendBuf.otherSideWindow = tcpHeader.WindowSize
	sendBuf.mu.Unlock()

	tableEntry.normalSocket.retransQueue = &RetransmissionQueue{
		head: 0,
		array: make([]*RetransmissionEntry, 0), // let grow dyna,ically with append
		rto: RTO_INIT, // refer to types comment
		// other fields can be null for now
	}

	table.mu.Unlock() /* UNLOCK MUTEX BEFORE CALLING SENDACK */
	tableEntry.sendAckHandshake(tcpHeader.SeqNum) /* passing in THEIR sequence num to ACK */
	/* set up send and recv buffers for comms */

	tableEntry.signalConnectResult(tableEntry.state) // unblock
	return nil
}

func (entry *SocketTableEntry) sendAckHandshake(passiveSeqNum uint32) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort, // TODO: dir should be right but verify some other time
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum, // already ++ in handle
		AckNum:        passiveSeqNum+1, 	/* Ack should be whatever we got from SYN + 1 */
		DataOffset:    20, 			/* TODO: same as other instances */
		Flags:         header.TCPFlagAck,
		WindowSize:    MAX_WIN_SIZE,
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
	/* enqueues request on tcpStack's request channel -> eventually calls SendTCP */
	entry.sendPacketFunc(sendReq)
	return nil
}

/* last step for passive side of connection (VAccept caller) -> init bufs after this */
func (tcp *TCPStack) handleAckHandshake(tableEntry *SocketTableEntry, tcpHeader header.TCPFields) error {
	if (tcpHeader.Flags & header.TCPFlagAck) == 0 {
		// not an ACK, drop	
		return nil
	}

	fmt.Println("received handshake ack")

	// lock
	table := tcp.socketTable
	table.mu.Lock()

	// verify ack has been updated correctly
	if tcpHeader.AckNum != tableEntry.seqNum+1 {
		table.mu.Unlock()
		return fmt.Errorf("AckNum does not match SeqNum; %d != %d", tcpHeader.AckNum, tableEntry.seqNum+1)
	}
	// if so, update passive side's seqNum in tableentry to be consistent
	tableEntry.seqNum += 1
	
	/* make sure we don't retransmit syn-ack */
	tableEntry.handshakeMu.Lock()
	fmt.Println("setting received SynAck to true")
	tableEntry.receivedSynAck = true
	tableEntry.handshakeMu.Unlock()
	tableEntry.stopHandshakeTimer() 

	// return state
	tableEntry.state = ESTABLISHED
	tableEntry.lastKnownAck = tcpHeader.AckNum

	/* init send/recv bufs before returning */
	/* pass in OTHER SIDE'S SEQ NUM for recv buffer -> next expected seqNum = same seqNum from ACK */
	// note per RFC - technically bufs are supposed to be init'd much earlier for coverage of weird edge cases (e.g. simulataneous open) that we don't need to worry about
	tableEntry.initBufs(tcpHeader.SeqNum)

	// init retransqueue, keep all other fields null for now
	tableEntry.normalSocket.retransQueue = &RetransmissionQueue{
		head: 0,
		array: make([]*RetransmissionEntry, 0),
		rto: RTO_INIT, // refer to types comment
	}
	tableEntry.normalSocket.socketID = tableEntry.socketID

	table.mu.Unlock() /* UNLOCK MUTEX BEFORE PASSING SOCKET */
	tableEntry.listenSocket.connChan <- tableEntry.normalSocket // unblock vconnnect
	table.listSockets()
	fmt.Println(">")
	return nil
}

func (entry *SocketTableEntry) sendSynRetrans() {
	entry.handshakeMu.Lock()
	// if received, no need to retrans
	if entry.state != SYN_SENT || entry.receivedSyn {
		entry.handshakeMu.Unlock()
		return
	}
	// if we hit maxTrans, close and remove
	if entry.numSynRetransmissions >= MAX_RETRANSMISSIONS {
		entry.state = CLOSED
		entry.handshakeMu.Unlock()

		entry.removeSelf(entry.socketID)
		entry.signalConnectResult(ERROR)
		fmt.Println("senderside handshake timeut")
		return
	}
	entry.numSynRetransmissions++
	retryNum := entry.numSynRetransmissions
	entry.handshakeMu.Unlock()
	fmt.Printf("retransmitting SYN for the %d time\n", retryNum)
	entry.sendSyn()
	entry.startSynRetransTimer()
}

func (entry *SocketTableEntry) sendSynAckRetrans(otherSideSeq uint32) {
	entry.handshakeMu.Lock()
	// if last ack received, no need to retrans
	if entry.state != SYN_RECEIVED || entry.receivedSynAck {
			entry.handshakeMu.Unlock()
			return
		}
	// if we hit maxTrans, close and remove
	if entry.numSynAckRetransmissions >= MAX_RETRANSMISSIONS {
		entry.state = CLOSED
		entry.handshakeMu.Unlock()

		entry.removeSelf(entry.socketID)
		fmt.Println("recvrside handshake timeout")
		return
	}
	entry.numSynAckRetransmissions++
	retryNum := entry.numSynAckRetransmissions
	entry.handshakeMu.Unlock()

	fmt.Printf("retransmitting SYNACK for the %d time\n", retryNum)
	entry.sendSynAck(otherSideSeq)
	entry.startSynAckRetransTimer(otherSideSeq)
}

func (entry *SocketTableEntry) startSynRetransTimer() {
	entry.handshakeMu.Lock()
	entry.handshakeTimer = time.AfterFunc(HANDSHAKE_TIMEOUT, func(){entry.sendSynRetrans()})
	entry.handshakeMu.Unlock()
}

func (entry *SocketTableEntry) startSynAckRetransTimer(otherSideSeq uint32) {
	entry.handshakeMu.Lock()
	entry.handshakeTimer = time.AfterFunc(HANDSHAKE_TIMEOUT, func(){entry.sendSynAckRetrans(otherSideSeq)})
	entry.handshakeMu.Unlock()
}

// nonblocking helper to unblock connect
func (entry *SocketTableEntry) signalConnectResult(state int) {
	select {
	case entry.establishedChan <- state:
	default:
	}
}

// helper to stop timer for handshake retransmissions
func (entry *SocketTableEntry) stopHandshakeTimer() {
	entry.handshakeMu.Lock()
	defer entry.handshakeMu.Unlock()

	if entry.handshakeTimer != nil {
		entry.handshakeTimer.Stop()
		entry.handshakeTimer = nil
	}
}
