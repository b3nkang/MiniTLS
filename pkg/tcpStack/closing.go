package tcpStack

import (
	"time"

	utils "ip-isabelle-and-ben/pkg/protocol"

	"github.com/google/netstack/tcpip/header"
)

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
	/* ASSUME THAT WHEN WE GET HERE, SENDBUF IS EMPTY -> HANDLE THAT IN VCLOSE */

	/* error if we aren't in established state -> this should be checked in close I think -> yes */
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort,
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		AckNum:        entry.normalSocket.recvBuf.nxt,
		DataOffset:    20,
		Flags:         header.TCPFlagFin|header.TCPFlagAck,
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

	/* enqueues request on tcpStack's request channel */
	entry.sendPacketFunc(sendReq)
	
	/* unlike acks, we also need to put this on retransmission queue
		this was NOT sent through sendLoop, so will need to add it manually here */
	utils.VPrintln("Adding FIN to retransmission queue")
	segmentRetransEntry := &RetransmissionEntry{
		seqNum: entry.seqNum,
		len: 0,
		flags: header.TCPFlagFin | header.TCPFlagAck,
		sent: time.Now(),
		retransmitted: false,
	}
	retransQueue := entry.normalSocket.retransQueue
	retransQueue.mu.Lock()

	if len(retransQueue.array) == 0 {
		utils.VPrintln("[TCP - sendloop] RTO: timer was stopped/set to 0; restarting")
		retransQueue.timer = entry.startRtoTimer()
	}
	retransQueue.array = append(retransQueue.array, segmentRetransEntry)
	retransQueue.mu.Unlock()

	/* update NXT and seqNum */
	entry.normalSocket.sendBuf.mu.Lock()
	entry.normalSocket.sendBuf.nxt += 1
	entry.seqNum = entry.normalSocket.sendBuf.nxt
	entry.normalSocket.sendBuf.mu.Unlock()

	/* switch state based on starting state */
	if entry.state == ESTABLISHED {
		entry.state = FIN_WAIT_1
	} else { /* must be CLOSE_WAIT */
		entry.state = LAST_ACK
	}
}

/* deal with checks regarding ACKING stored FIN from handlePayload after loading segments from early arrivals
	- this function is only called if early arrivals heap is empty and fin WAS early
	ENTER WITH MUTEX LOCKED, LEAVE WITH MUTEX LOCKED
*/
func (entry *SocketTableEntry ) handleEarlyFin() {
	recvBuf := entry.normalSocket.recvBuf
	recvBuf.mu.Lock()
	/* if we haven't received FIN */
	if recvBuf.fin == 0 { 
		recvBuf.mu.Unlock()
		return
	}

	// if FIN is still not the next expected seq, just leave quietly
	if recvBuf.fin != recvBuf.nxt {
		recvBuf.mu.Unlock()
		return
	}

	/* we have received FIN, check if FIN is our nxt pointer and ACK fin + switch state */
	utils.VPrintln("FIN seq num is what we expect for next, ACKING FIN and switching state")
	finAckNum := recvBuf.fin + 1 // need bc after unlock there can be race condition
	recvBuf.nxt = finAckNum
	recvBuf.fin = 0
	recvBuf.mu.Unlock()
	/* only send ACK if fin's seq num is expected */
	entry.sendPureAck(finAckNum)

	/* PASSIVE SIDE */
	switch entry.state {
	case ESTABLISHED:
		utils.VPrintln("Handled FIN when it arrived early and we are in ESTABLISHED. Entering CLOSE_WAIT")
		entry.state = CLOSE_WAIT /* wait for app to call close */

		select { // signal to blocked VRead so it can get to CLOSE_WAIT and ret io.EOF
		case entry.normalSocket.recvBuf.dataToRead <- struct{}{}:
		default:
		}

	/* ACTIVE SIDE */
	case FIN_WAIT_1,FIN_WAIT_2:
		utils.VPrintln("Handled FIN when it arrived early and we are in FIN_WAIT_1/FIN_WAIT_2. Entering TIME_WAIT")
		entry.state = TIME_WAIT
		entry.timeWait()
	case CLOSE_WAIT, LAST_ACK, TIME_WAIT: // duplicate/late FIN-ish case; ignore
		return
	default:
		return
	}
}

func (entry *SocketTableEntry) handleFin(tcpHdr header.TCPFields) {
	utils.VPrintln("entered handleFin function")
	recvBuf := entry.normalSocket.recvBuf
	/* 	- RECEIVE FIN
		- send ACK that we got that FIN (ACKnum is that seqNum + 1)
		- switch to state CLOSE_WAIT */
	recvBuf.mu.Lock()
	/* set recvBuf FIN field with this FIN so that we know we've gotten it */
	recvBuf.fin = tcpHdr.SeqNum

	/* check if FIN is early arrival and just return -> handlePayload will ACK FIN and switch state later */
	if tcpHdr.SeqNum > recvBuf.nxt {
		recvBuf.mu.Unlock()
		return
	} 
	// else if tcpHdr.SeqNum != recvBuf.nxt {
	// 	fmt.Println("Error: weirdness in handleFin because sqnNum was less than RecvBuf.nxt")
	// 	recvBuf.mu.Unlock()
	// 	return
	// }

	// duplicate/old FIN: ACK again so the other side stops retransmitting FIN
	if tcpHdr.SeqNum < recvBuf.nxt {
		ackNum := recvBuf.nxt
		recvBuf.mu.Unlock()
		entry.sendPureAck(ackNum)
		return
	}

	/* increment recvBuf.nxt -> not sure if we actually need to do this , but we 
		now know we will never receive more data from the other side */
	finAckNum := tcpHdr.SeqNum + 1
	recvBuf.nxt = finAckNum
	recvBuf.fin = 0 // consume it now that we are handling it
	recvBuf.mu.Unlock()

	/* send ACK since we know fin's seq num is expected */
	entry.sendPureAck(finAckNum)

	/* PASSIVE SIDE */
	switch entry.state {
	case ESTABLISHED:
		utils.VPrintln("Handled FIN when it arrived on time and we are in ESTABLISHED. Entering CLOSE_WAIT")
		entry.state = CLOSE_WAIT /* wait for app to call close */

		select { // signal to blocked VRead so it can get to CLOSE_WAIT and ret io.EOF
		case entry.normalSocket.recvBuf.dataToRead <- struct{}{}:
		default:
		}

	/* ACTIVE SIDE */
	case FIN_WAIT_1,FIN_WAIT_2:
		utils.VPrintln("Handled FIN when it arrived on time and we are in FIN_WAIT_1/FIN_WAIT_2. Entering TIME_WAIT")
		entry.state = TIME_WAIT
		entry.timeWait() /* go and wait and then close conn */
	case CLOSE_WAIT, LAST_ACK, TIME_WAIT: // late duplicate FIN case, ignore
		return
	default:
		return
	}
}

/* called when we recieve FIN and are in FIN_WAIT_2 state -> start timer to wait to close connection and then close it */
func (entry *SocketTableEntry) timeWait() {
	/* start timer */
	time.AfterFunc((2*MAX_SEGMENT_LATENCY)*time.Second, func() {
		entry.state = CLOSED /* this should signal to all routines to return */
		entry.teardown()
	})
}

/* at this point, we are in last ack state and need to do teardown */
func (entry *SocketTableEntry) handleLastAck(){
	entry.state = CLOSED
	entry.teardown()
}

/* cleanup all resources associated with this socket -> close channels, remove from TCB, stop
	goroutines */
func (entry *SocketTableEntry) teardown() {
	utils.VPrintln("[TEARDOWN] entered teardown function, removing entry from table")
	/* close channels */
	close(entry.normalSocket.packetChan)

	entry.normalSocket.sendBuf.mu.Lock()
	close(entry.normalSocket.sendBuf.dataWrittenToBuf)
	close(entry.normalSocket.sendBuf.spaceAvailable)
	entry.normalSocket.sendBuf.mu.Unlock()
	// utils.VPrintln("Did not deadlock on the sendBuf")

	entry.normalSocket.recvBuf.mu.Lock()
	close(entry.normalSocket.recvBuf.dataToRead)
	entry.normalSocket.recvBuf.mu.Unlock()
	// utils.VPrintln("Did not deadlock on the recv buf")
	/* remove self from socketTable */
	entry.removeSelf(entry.socketID)
}

func (entry *SocketTableEntry) listenerTeardown() {
	/* close channels */
	close(entry.listenSocket.connChan)
	/* remove from socket table */
	entry.removeSelf(entry.socketID)
}

/* remove entry from socket table -> referenced as removeSelf from tableEntry */
func (table *SocketTable) Remove(socketID int) {
    table.mu.Lock()
    delete(table.socketMap, socketID)
	table.mu.Unlock()
}


