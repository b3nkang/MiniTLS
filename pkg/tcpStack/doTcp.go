package tcpstack

/*

RUNNING BUGS/PROBLEMS LIST:
- (really high priority) deadlock on send buffer in teardown for PASSIVE CLOSER
- (high priority) if we send a SYN to the wrong IP/port (i.e they don't exist)
	we create an entry in the table that never moves past SYN-SENT state and is never removed
- (low priority) busy-wait for send buf to be empty in VClose
- (mid priority) weird retransmission thing where it tries to retransmit and queue is empty (shouldn't be happening)



For state stuff:
- don't worry about stuff that couldn't happen in this project

if FIN arrives early:
- save state for "seen FIN" and check at the end of early arrivals queue check

For timeout case:
- print something, delete socket/connection and table entry, that's it

Had to do something a little gross for closing because VRead and VWrite need to know about state.
This should also help us a LOT going forward if it's kosher.
I added a pointer to socketTableEntry in Conn
So now SocketTableENtry stores Conn, but Conn also has a pointer for SocketTableEntry...idk

NOTES:
	MSS is 1360 i think? maybe we should verify this...Verified: we can set MSS to whatever we want!

TODO: TIMEOUT CONNECTION AFTER X RETRANSMISSIONS (rfc MUST-20): Can just pick a maximum number of retransmissions, abort if this threshold is exceeded.
Value does not need to be associated with a specific time interval (though you may want to set a minimum time interval, eg. 5s, before the connection aborts)

TODO: RFC MUST-66: Receiving an RST MUST always immediately terminate the connection.  Can always ignore URG flag.

TODO: read through socket API description in handout and make sure we're returning errors properly

// TODO: small, refactor tcpHeader = SEGheader or something to make things much less confusing
// TODO: low prio/long term, look into if seqNum field is even necessary in table entry. may not be

*/

import (
	"errors"
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

/* handler function that receives all TCP messages
not sure if we need the IP header here but will leave for now
*/
func (tcp *TCPStack) HandleTCP(hdr *ipv4header.IPv4Header, payload []byte) {
	table := tcp.socketTable

	/* 1. parse TCP header and extract message body */
	tcpHdr, tcpPayload, err := utils.ParseAndValidateTCP(hdr, payload) // TODO: re-add tcpData when we need. Its "_" right now because no use, so to turn off compiler warning
	if err != nil {
		/* checksum failed */
		fmt.Printf("Error: %s\n", err.Error())
	}

	/* 2. match tuple to our table; tableMatch(srcPort, srcIP, destPort, destIP) */
	socketEntry, err := table.tableMatch(tcpHdr.SrcPort, hdr.Src, tcpHdr.DstPort, hdr.Dst)
	if err != nil {
		/* didn't find match */
		fmt.Println("[TCP] No normal or listener socket open that matches the following:")
		fmt.Printf("Src Port: %s\nSrc IP: %s\n Dest Port: %s\n Dest IP: %s\n", string(tcpHdr.SrcPort), hdr.Src.String(), string(tcpHdr.DstPort), hdr.Dst.String())
		return
	} 
	
	// PrintSocketTableEntry(socketEntry)

	/* 3. act differently based on state of that conn in our table */
	switch socketEntry.state {
	case LISTEN:
		/* we matched the listen socket--so we should be getting an initial SYN */
		/* should pass in IP Source as OUR DEST and IP Dest as OUR SOURCE since this is FROM REMOTE */
		tcp.handleSyn(socketEntry.listenSocket, tcpHdr, hdr.Dst, hdr.Src)
		return
	case SYN_RECEIVED:
		tcp.handleAckHandshake(socketEntry, tcpHdr)
	case SYN_SENT:
		tcp.handleSynAck(socketEntry, tcpHdr)
	case ESTABLISHED, FIN_WAIT_2, FIN_WAIT_1, CLOSE_WAIT:
		/* FIN_WAIT_2 is valid here because we can still receive packets */
		switch {
		/* FIN specified -> we are passive side and should ACK that FIN */
		case tcpHdr.Flags & header.TCPFlagFin != 0:
			socketEntry.handleFin(tcpHdr) /* this will either ACK fin or tell recvBuffer we got FIN early */
			return
		/* data ACK */
		case tcpHdr.Flags & header.TCPFlagAck != 0 && len(tcpPayload) == 0:
			if socketEntry.state == FIN_WAIT_2 {
				fmt.Println("weirdness--we should not be getting an ack in FIN_WAIT_2")
			}
			fmt.Printf("[TCP - HandleTCP] received ACK for seg: %d\n", tcpHdr.AckNum)
			socketEntry.handlePureAck(tcpHdr)
			return
		/* invalid/empty packet that isn't FIN or pure ACK */
		case len(tcpPayload) < 1:
			fmt.Println("[TCP - HandleTCP] recvd full empty packet, dropping")
			return
		}
		// note - may be other flags to handle in this case, to add if so
		socketEntry.handlePayload(tcpHdr, tcpPayload) // put into recvBuf and handle all effects
	case TIME_WAIT:
		fmt.Println("Error: Received packet in TIME_WAIT state. Dropping.")
		return
	case LAST_ACK:
		/* if we are in this state, both sides are done sending data -> 
		this is our cue to close everything -> we should ONLY receive an ACK */
		if !(tcpHdr.Flags & header.TCPFlagAck != 0 && len(tcpPayload) == 0) {
			fmt.Println("Error: received a non-ACK packet in LAST_ACK state")
			return
		} else {
			socketEntry.handlePureAck(tcpHdr)
		}
	default:
		fmt.Printf("No known state that matches: %d\n", socketEntry.state)
	}
}

/* send out packets via sendTCP as they come in */
func (tcp *TCPStack) sendPacketsOut() {
	for req := range tcp.sendRequests {
		tcp.sendTCP(req)
	}
}

/* match an incoming packet to a table entry */
func (table *SocketTable) tableMatch(srcPort uint16, srcIP netip.Addr, destPort uint16, destIP netip.Addr) (*SocketTableEntry, error) {
	var listenerMatch *SocketTableEntry

	/* iterate through table entries */
	for _, entry := range table.socketMap {

		/* exact match: not a listener + all fields match */
		if entry.state != LISTEN &&
			entry.localPort == destPort &&
			entry.destPort == srcPort &&
			entry.localIP == destIP &&
			entry.destIP == srcIP {
			return entry, nil
		}

		/* found a listener match: store just in case */
		if entry.state == LISTEN && entry.localPort == destPort {
			listenerMatch = entry
		}
	}

	/* if we didn't return an exact match, return the listener */
	if listenerMatch != nil {
		return listenerMatch, nil
	}

	/* otherwise, we didn't find a match */
	return nil, errors.New("no match found in socket table")
}

/* 
	Calls SendIP where message is the TCP packet
	takes in TCP Header because otherwise params would be insane

	TODO: probably implement timeouts here for Milestone 1? I think that's in the handout
		// TODO: reply from ben, i think not per this from handout:
				For this milestone, you SHOULD NOT attempt to implement retransmissions for dropped 
				handshake packets. Instead, we recommend leaving this for the final stage, when you’ll 
				build a generic implementation for retransmissions that works with data packets too.

*/
func (tcp *TCPStack) sendTCP(sendReq *SendRequest) {
	
	hdr := sendReq.tcpHeader
	srcIP := sendReq.sourceIP
	destIP := sendReq.destIP
	data := sendReq.data
	
	/* compute TCP checksum */
	checksum := utils.ComputeTCPChecksum(hdr, srcIP, destIP, data)
	hdr.Checksum = checksum

	// Serialize the TCP header
	tcpHeaderBytes := make(header.TCP, utils.TcpHeaderLen)
	tcpHeaderBytes.Encode(hdr)

	/* Combine the TCP header + payload into one byte array, which becomes the payload of the IP packet */
	ipPacketPayload := make([]byte, 0, len(tcpHeaderBytes)+len(data))
	ipPacketPayload = append(ipPacketPayload, tcpHeaderBytes...)
	ipPacketPayload = append(ipPacketPayload, []byte(data)...)

	/* call SendIP */
	tcp.ipStack.SendIP(destIP, ipPacketPayload, 6)
	/* TODO: return bytes written/sent? */
}

/* initialize send and receive buffers in Conn obj */
func (entry *SocketTableEntry) initBufs(otherSideSeq uint32) {
	sendCBuf := NewCircleBuf(MAX_WIN_SIZE, entry.seqNum)
	sendBuf := &SendBuf{
		cBuf: sendCBuf,
		dataWrittenToBuf: make(chan struct{}, 1),
		spaceAvailable: make(chan struct{}, 1),
		otherSideWindow: MAX_WIN_SIZE,
	}
	recvCBuf := NewCircleBuf(MAX_WIN_SIZE, otherSideSeq)
	recvBuf := &RecvBuf{
		cBuf: recvCBuf,
		dataToRead: make(chan struct{}, 1), 
		fin: 0,
	}

	/* init pointers */
	ourSeqNum := entry.seqNum

	/* send buf --uses OUR (this side's) sequence numbers */
	sendBuf.una = ourSeqNum // keep for now until end of mstone2. see UNA field in types for reasoning. TODO: verify if needed after mstone2
	sendBuf.nxt = ourSeqNum /* next sequence num to send */
	sendBuf.lbw = ourSeqNum-1 /* last byte written by app, next write starts at lbw+1 */

	/* recv buf -- uses OTHER SIDE's sequence numbers --> nxt is the next expected SEQ from THEIR SIDE */
	recvBuf.lbr = otherSideSeq-1 /* last SEQ NUM read by app (start next read from this + 1) */
	recvBuf.nxt = otherSideSeq /* next sequence num expected from sender */

	conn := entry.normalSocket
	conn.sendBuf = sendBuf
	conn.recvBuf = recvBuf

	/* create receive buf's early arrivals min-heap */
	recvBuf.earlyArrivals = MakeEarlyArrivals()

	/* start threads for sending and receiving */
	go entry.sendLoop()

}

// ----------- buffer logic ------------

/* put data in recv buffer 

Per RFC: 

When data is received, the following comparisons are needed:
RCV.NXT = next sequence number expected on an incoming segment, and is the left or lower edge of the receive window
RCV.NXT+RCV.WND-1 = last sequence number expected on an incoming segment, and is the right or upper edge of the receive window
SEG.SEQ = first sequence number occupied by the incoming segment
SEG.SEQ+SEG.LEN-1 = last sequence number occupied by the incoming segment
A segment is judged to occupy a portion of valid receive sequence space if
RCV.NXT =< SEG.SEQ < RCV.NXT+RCV.WND
or
RCV.NXT =< SEG.SEQ+SEG.LEN-1 < RCV.NXT+RCV.WND

*/
func (entry *SocketTableEntry) handlePayload(tcpHeader header.TCPFields, payload []byte) error {
	// flag is flipped to get recvr to drop all packets for the purpose of testing retransmissions
	if entry.dropForRetrans {
		/* for testing: only drop packet 50% of the time */
		if time.Now().UnixNano()%2 == 0 {
			fmt.Printf("[TCP - handlePayload] flag dropForRetrans + coin flip = true, DROPPING SEGMENT %d\n", tcpHeader.SeqNum)
			return nil
		}
	}

	// prior logic already handles empty payloads, assume len(payload) > 0
	recvBuf := entry.normalSocket.recvBuf

	recvBuf.mu.Lock()

	/* early arrival case -- seqNum of segment greater than NXT */
	if tcpHeader.SeqNum > recvBuf.nxt {
		fmt.Printf("[TCP HANDLE PAYLOAD] Expected Seq: %d, Got Seq: %d, adding to Early Arrivals Heap\n", recvBuf.nxt, tcpHeader.SeqNum)
		/* add segment to early arrivals heap */
		recvBuf.earlyArrivals.PushSegment(tcpHeader.SeqNum, payload)
		recvBuf.mu.Unlock()

		/* send Ack */
		entry.sendPureAck(recvBuf.nxt)
		return nil
	}

	/* old/redundant segment -- seqNum of segment less than NXT - TODO technically we are supposed to discard payload and send an ack back*/
	if tcpHeader.SeqNum < recvBuf.nxt {
		fmt.Println("[TCP HANDLE PAYLOAD] Got redundant segment, dropping packet")
		recvBuf.mu.Unlock()
		return nil
	}

	/* else, must be the segment number we're looking for */

	/* quick space check before writing */
	space := int(recvBuf.cBuf.FreeSpace())
	if space == 0 {
		fmt.Println("[TCP - handlePayload] RECVBUF_SPACE=0, sending back ZWP ack")
		recvBuf.mu.Unlock()
		entry.sendPureAck(recvBuf.nxt)
		return nil
	}
	if len(payload) > space {
		fmt.Printf("[TCP - handlePayload] ERROR: payload length longer than free space in recv buf! Sender side should now allow this. Writing truncated version of payload into recv buf")
		payload = payload[:space]
	}

	bytesWritten := recvBuf.cBuf.WriteIntoBuf(recvBuf.nxt, payload)
	if bytesWritten != len(payload) { /* will never get here right now */
		fmt.Printf("[TCP - handlePayload] only wrote %d/%d bytes into recv buffer\n",bytesWritten,len(payload))
		recvBuf.mu.Unlock()
		return nil
	}
	
	payLen := uint32(len(payload))
	
	recvBuf.nxt += payLen	/* next updated here! don't move lbr until READ() */

	/* drain early arrival heap of any segments that can now fit */
	for {
		min := recvBuf.earlyArrivals.Peek()
		/* if nothing in heap -> no early arrivals or we have already drained it -> check for FIN before breaking */
		if min == nil {
			/* checks if we have early FIN and deals with it if necessary */
			recvBuf.mu.Unlock()
			entry.handleEarlyFin() /* handle early fin needs to lock mutex-> give it mu unlocked*/
			recvBuf.mu.Lock()
			break
		}

		/* if our min seq num is not our nxt pointer, break */
		if min.startSeq != recvBuf.nxt {
			break
		}
		/* if not enough space in the recv buffer for full segment, just abandon ship 
			TODO--verify if that is okay 
			- this means we will advertise win=X where X < Max Segment Size
			- sender will send another segment of that size and then ZWP will start so we're good */
		space := int(recvBuf.cBuf.FreeSpace())
		if len(min.data) > space {
			fmt.Print("Got to case where first segment in early arrivals is longer than receive buf available space. Space in buf: %d, Size of segment: %d\n",
						space, len(min.data))					
			break
		}

		fmt.Printf("Popping early arrival from queue: %s\n", min.data)

		/* actually take out minimum segment and write to recv buffer */
		segment := recvBuf.earlyArrivals.PopMin()
		numBytesWritten := recvBuf.cBuf.WriteIntoBuf(segment.startSeq, segment.data)
		recvBuf.nxt += uint32(numBytesWritten)
	}

	/* ------ print receive buffer for debugging ----- */
	// fmt.Printf("state: base=%d nxt=%d lbr=%d currSize=%d payLen=%d\n", recvBuf.cBuf.baseSeq, recvBuf.nxt, recvBuf.lbr, recvBuf.cBuf.currSize, payLen)
	fmt.Printf("copied bytes: %q\n", recvBuf.cBuf.SliceFrom(tcpHeader.SeqNum, payLen))
	if recvBuf.cBuf.currSize > 0 {
		fmt.Printf("entire readable region: %q\n",recvBuf.cBuf.SliceFrom(recvBuf.lbr+1, recvBuf.cBuf.currSize))
	} else {
		fmt.Printf("entire readable region: %q\n", []byte{})
	}
	/* ------------------------------------------------ */

	recvBuf.mu.Unlock()

    select {
	case recvBuf.dataToRead <- struct{}{}:
	default:
	}
	entry.sendPureAck(recvBuf.nxt)
	return nil
}

// send "pure" ack, i.e. no payload, passive side sends
func (entry *SocketTableEntry) sendPureAck(otherSideSeq uint32) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort, // TODO: verify
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		AckNum:        otherSideSeq,
		DataOffset:    20,
		Flags:         header.TCPFlagAck,
		WindowSize:    entry.normalSocket.recvBuf.getAvailableWindow(),
		Checksum:      0,
		UrgentPointer: 0,
	}

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

/* creates tcp header and calls send method to send segment to receiving end of conn */
func (entry *SocketTableEntry) sendSegment(segment []byte, seqNum uint32, flags uint8) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort,
		DstPort:       entry.destPort,
		SeqNum:        seqNum,
		AckNum:        entry.normalSocket.recvBuf.nxt,
		DataOffset:    20,
		Flags:         flags, /* ACK is always set after conn established */
		WindowSize:    entry.normalSocket.recvBuf.getAvailableWindow(),
		Checksum:      0,
		UrgentPointer: 0,
	}

	sendReq := &SendRequest{
		tcpHeader: tcpHdr,
		data: segment,
		sourceIP: entry.localIP,
		destIP: entry.destIP,
	}
	/* enqueues request on tcpStack's request channel -> eventually calls SendTCP */
	entry.sendPacketFunc(sendReq)

	// // NOTE: not super sure if this works, i would rather put it close to where we increment
	// // 		SND.NXT in sendloop. per RFC the SND.NXT pointer is how we track seqNum on sender side
	// /* update our sequence number to reflect segment being sent */
	// entry.seqNum += uint32(len(segment))

	return nil
}

/*
ESTABLISHED STATE

If SND.UNA < SEG.ACK =< SND.NXT, then set SND.UNA <- SEG.ACK. 
Any segments on the retransmission queue that are thereby entirely acknowledged are removed. 
Users should receive positive acknowledgments for buffers that have been SENT and fully 
acknowledged (i.e., SEND buffer should be returned with "ok" response). If the ACK is a 
duplicate (SEG.ACK =< SND.UNA), it can be ignored. If the ACK acks something not yet sent 
(SEG.ACK > SND.NXT), then send an ACK, drop the segment, and return.

If SND.UNA =< SEG.ACK =< SND.NXT, the send window should be updated. If (SND.WL1 < SEG.SEQ or 
(SND.WL1 = SEG.SEQ and SND.WL2 =< SEG.ACK)), set SND.WND <- SEG.WND, set SND.WL1 <- SEG.SEQ, 
and set SND.WL2 <- SEG.ACK.

Note that SND.WND is an offset from SND.UNA, that SND.WL1 records the sequence number of the
last segment used to update SND.WND, and that SND.WL2 records the acknowledgment number of the 
last segment used to update SND.WND. The check here prevents using old segments to update the 
window
*/

// receive a pure ack and update our side's fields accordingly.
// 		currently, this consists of:
//			- sendBuf.otherSideWindow, tracking of the other side's available window size
// 			- sendBuf.una, we may want to use an enqueue-inflight-data structure for retrans but for now it is needed (TODO: revisit)
func (socketEntry *SocketTableEntry) handlePureAck(seg header.TCPFields) error {
	sendBuf := socketEntry.normalSocket.sendBuf
	sendBuf.mu.Lock()
	defer sendBuf.mu.Unlock()

	// ------------------  TODO: test edge cases once circular array is up --------------

	if seg.AckNum > sendBuf.nxt { // RFC: If the ACK acks something not yet sent (SEG.ACK > SND.NXT), then send an ACK, drop the segment, and return
		// TODO: implement a sendPureAckForSender(). this is a bit of a pain, since sendPureAck() is for the recv side and thus we need a new version
		fmt.Println("[TCP - handlePureAck] condition seg.AckNum > sendBuf.nxt, no fix implemented yet, dropping")
		return nil
	}

	// update immediately for zwp
	sendBuf.otherSideWindow = seg.WindowSize // update new window size
	fmt.Printf("[TCP - handlePureAck] sender got new recvr window size: %d\n", sendBuf.otherSideWindow)

	// RFC: SND.UNA < SEG.ACK <= SND.NXT -> ACK num of segment is less than or equal to our next Sequence Num (in our send buf)
	if sendBuf.una < seg.AckNum && seg.AckNum <= sendBuf.nxt {
		// fmt.Println("[TCP - handlePureAck] adjusting UNA to seg.AckNum")
		ackedBytes := seg.AckNum - sendBuf.una /* num bytes accounted for via this ACK */
		sendBuf.una = seg.AckNum /* move UNA up */
		/* adjust internals of circular buffer to reflect num bytes Acked */
		sendBuf.cBuf.AdvanceBase(ackedBytes)

		// --------------- retransmission queue updates ------------------
		//	RFC 6298 (5):
		//    An implementation MUST manage the retransmission timer(s) in such a
		//    way that a segment is never retransmitted too early, i.e., less than
		//    one RTO after the previous transmission of that segment.

		//    The following is the RECOMMENDED algorithm for managing the
		//    retransmission timer:

		//    (5.1) Every time a packet containing data is sent (including a
		//          retransmission), if the timer is not running, start it running
		//          so that it will expire after RTO seconds (for the current value
		//          of RTO).

		//    (5.2) When all outstanding data has been acknowledged, turn off the
		//          retransmission timer.

		//    (5.3) When an ACK is received that acknowledges new data, restart the
		//          retransmission timer so that it will expire after RTO seconds
		//          (for the current value of RTO).

		retransQueue := socketEntry.normalSocket.retransQueue
		retransQueue.mu.Lock()

		// stop the timer since we just got something ack'd
		retransQueue.timer.Stop()

		var ackedEntry *RetransmissionEntry

		// NOTE FOR ZWP: 	we DO NOT need to slice off one byte from the head of the RQ when a ZWP byte gets acked because 
		//					there will NEVER be data in flight when we enter ZWP (recv cannot ack ZWP byte if data in flight)
		
		// update queue head: pop from head repeatedly until we get the segement we just ack'd popped
		for len(retransQueue.array) > 0 && retransQueue.array[0].seqNum + retransQueue.array[0].len <= seg.AckNum {
			ackedEntry = retransQueue.array[0] // we need entry's .sent time field to calculate RTT
			// the ack is still ahead of the head of the queue, keep popping
			retransQueue.array = retransQueue.array[1:]
		}

		// update RTO
		if ackedEntry != nil && !ackedEntry.retransmitted { // if duplicate ack, ackedEntry will be null, and if retrans we don't want to update (Karn's)
			fmt.Printf("[TCP - handlePureAck] RTO: updating RTO given new recvd ack for seg %d\n", ackedEntry.seqNum)
			err := retransQueue.updateRto(ackedEntry.getRtt())
			if err != nil {
				fmt.Println("[TCP - handlePureAck] error: update RTO failed")
			}
		}

		// 5.2: if the queue is now empty, we stop and do nothing (we previously stopped it earlier in this function)
		// 5.3: if queue still has data in flight (entries), then restart the timer
		if len(retransQueue.array) > 0 {
			fmt.Println("[TCP - handlePureAck] 5.3 RTO: still data in flight, restarting timer")
			retransQueue.timer = socketEntry.startRtoTimer()
		}

		retransQueue.mu.Unlock()

		/* check if we are closing and our FIN got ACKED, switch state
			we should not receive more ACKs again , and FIN should have just been
			removed from retransmission queue */
		if socketEntry.state == FIN_WAIT_1 && seg.AckNum == sendBuf.nxt {
			fmt.Println("Received ACK for FIN as Active Closer. Switching to FIN_WAIT_2")
			socketEntry.state = FIN_WAIT_2
		}

		if socketEntry.state == LAST_ACK && seg.AckNum == sendBuf.nxt {
			fmt.Println("Received ACK for FIN as Passive Closer. Switching to CLOSED state")
			/* go deal with teardown now and DON'T try and tell sendBuf that there is space */
			sendBuf.mu.Unlock()
			socketEntry.handleLastAck()
			sendBuf.mu.Lock()
			return nil
		}


		/* tell sendBuf that there is space */	
		select {
		case sendBuf.spaceAvailable <- struct{}{}:
		default:
		}
	} else if seg.AckNum <= sendBuf.una { // RFC: If the ACK is a duplicate (SEG.ACK =< SND.UNA), it can be ignored.
		// we also don't need to update retransQueue since the out-of-order segment will have sliced this segment off already
		return nil
	} else {
		fmt.Println("[TCP - handlePureAck] condition should not have been hit")
		return nil
	}
	select {
	case sendBuf.otherSideWindowUpdated <- struct{}{}:
	default:
	}	
	return nil
}

/* ONLY FOR SENDING DATA (NO FINS OR ACKS ) thread that waits on data in the buffer and sends said DATA when it's there 
   TODO: verify there is enough space in the buffer to send. this will rely
   on some sort of field/data structure that DOES NOT EXIST YET -> need to 
   know other side's window size and update it with Acks */
func (entry *SocketTableEntry) sendLoop() {
	conn := entry.normalSocket

	var probeTimer *time.Timer
	probing := false

	for {
		/* return if state is closed */
		if entry.state == CLOSED {
			return
		}
		/* wait for data to be put in buffer by VWrite */
		<-conn.sendBuf.dataWrittenToBuf

		for { /* added second loop here to deal with MSS (keep sending until all data sent) */

			/* lock mutex so nothing changes rn */
			conn.sendBuf.mu.Lock()
			sendBuf := conn.sendBuf

			// if not necessary, pass
			if sendBuf.lbw < sendBuf.nxt {
				fmt.Println("[TCP - sendLoop] no unsent data")
				conn.sendBuf.mu.Unlock()
				break
			}

			bytesAvailableInBuf := sendBuf.lbw-sendBuf.nxt+1 	// NOTE: lbw is INCLUSIVE, meaning that we MUST add +1
																// 		 since LBW points to a byte that we need to send also
			fmt.Printf("[TCP - sendLoop] state: base=%d una=%d nxt=%d lbw=%d currSize=%d bytesAvail=%d otherSideWindow=%d\n",
						sendBuf.cBuf.baseSeq,sendBuf.una,sendBuf.nxt,sendBuf.lbw,sendBuf.cBuf.currSize,bytesAvailableInBuf,sendBuf.otherSideWindow)

			/* PRINTING whole send buffer */
			if sendBuf.cBuf.currSize > 0 {
				fmt.Printf("entire live send buffer: %q\n",sendBuf.cBuf.SliceFrom(sendBuf.cBuf.baseSeq, sendBuf.cBuf.currSize))
			} else {
				fmt.Printf("entire live send buffer: %q\n", []byte{})
			}

			// CHECK HOW MUCH WE CAN SEND: min(SND.LBW - SND.NXT, SND.WND - (SND.NXT-SND.UNA))
			// 			where SND.LBW - SND.NXT is just everything in the buffer to-be-sent
			//			where SND.WND - (SND.NXT-SND.UNA) is otherSideWindow - bytesInFlight


			/* calculate amount of space in receiver's receive buf = other window size - bytes in flight */
			windowRemaining := int(sendBuf.otherSideWindow) - int(sendBuf.getBytesInFlight())
			fmt.Printf("[TCP - sendloop] Other side window: %d, Bytes in flight: %d, Window Remaining (OSW - BIF) = %d\n", 
						sendBuf.otherSideWindow, sendBuf.getBytesInFlight(), windowRemaining)

			// if no windowRemaining, we will always continue and restart the inner for loop
			if windowRemaining <= 0 {
				fmt.Println("[TCP - sendloop] no window left")
				/* start zero-window-probing here! */
				// 		if lbw < nxt there is no data to send
				//		bytesInFlight must be 0
				if int(sendBuf.otherSideWindow) == 0 && sendBuf.lbw >= sendBuf.nxt && sendBuf.getBytesInFlight() == 0 {
					fmt.Println("[TCP - sendloop] starting ZWP")

					// only send a probe when: first entering ZWP, or after timer retriggers
					if !probing {
						probeSeq := sendBuf.una // una and nxt should be the same (no data in flight)
						probeByte := sendBuf.cBuf.SliceFrom(probeSeq, 1)
						sendBuf.mu.Unlock()

						// send probe
						entry.sendSegment(probeByte, probeSeq, header.TCPFlagAck)

						// start timer for next ZWP
						// small TODO: add exponential backoff (not strictly required in spec)
						if probeTimer == nil {
							probeTimer = time.NewTimer(PROBE_ITV)
						} else {
							probeTimer.Reset(PROBE_ITV)
						}
						probing = true
					} else {
						sendBuf.mu.Unlock()
					}

					select {
					case <-sendBuf.otherSideWindowUpdated:
						// a new ack has arrived with a new window size
						continue
					case <-probeTimer.C:
						// time to probe again
						probing = false // this will cause us to probe on next loop
						continue
					}
				} else {
					// when we start ZWP there should never be bytes in flight, so if there are, wait for retransmissions to clear them first
					fmt.Println("[TCP - sendloop] conditions not met for ZWP, waiting on retransmission")
					sendBuf.mu.Unlock()
					// we don't want to busy wait, so block until we get an update on the window
					<-sendBuf.otherSideWindowUpdated
					continue
				}
			}

			maxBytesSendable := bytesAvailableInBuf
			if int(maxBytesSendable) > windowRemaining {
				fmt.Printf("Decreasing sendable bytes (%d) to match window remaining (%d)\n", maxBytesSendable, windowRemaining)
                maxBytesSendable = uint32(windowRemaining)
            }
            if maxBytesSendable > MAX_SEG_SIZE {
				fmt.Printf("Decreasing sendable bytes (%d) to match max segment size (%d)\n", maxBytesSendable, MAX_SEG_SIZE)
                maxBytesSendable = MAX_SEG_SIZE
            }

			segmentData := sendBuf.cBuf.SliceFrom(sendBuf.nxt, uint32(maxBytesSendable))
			conn.sendBuf.mu.Unlock()

			if entry.sendSegment(segmentData, entry.seqNum, header.TCPFlagAck) != nil {
				fmt.Println("[TCP - Send Loop] Error sending segment")
				break
			}

			// add to retransmission queue
			segmentRetransEntry := &RetransmissionEntry{
				seqNum: entry.seqNum,
				len: uint32(maxBytesSendable),
				flags: header.TCPFlagAck,
				sent: time.Now(),
				retransmitted: false,
			}
			retransQueue := entry.normalSocket.retransQueue
			retransQueue.mu.Lock()

			// RFC 6298:
			//    (5.1) Every time a packet containing data is sent (including a
			//          retransmission), if the timer is not running, start it running
			//          so that it will expire after RTO seconds (for the current value
			//          of RTO).
			if len(retransQueue.array) == 0 {
				fmt.Println("[TCP - sendloop] RTO: timer was stopped/set to 0; restarting")
				retransQueue.timer = entry.startRtoTimer()
			}
			
			retransQueue.array = append(retransQueue.array, segmentRetransEntry)
			retransQueue.mu.Unlock()

            conn.sendBuf.mu.Lock()
			/* update NXT and seqNum */
            sendBuf.nxt += maxBytesSendable
            entry.seqNum = sendBuf.nxt
            conn.sendBuf.mu.Unlock()

			/* loop again--will break if no data in buffer */
		}
	}
}

/* tiny helpers */

func (sendBuf *SendBuf) getBytesInFlight() uint32 {
	return sendBuf.nxt - sendBuf.una
}

/* DON'T count early arrivals toward overall window size */
func (recvBuf *RecvBuf) getAvailableWindow() uint16 {
	return uint16(MAX_WIN_SIZE - recvBuf.cBuf.currSize)
}

// highest-level RTO countdown function. calls retransmitSegment if timer expires
func (entry *SocketTableEntry) startRtoTimer() *time.Timer {
	// TODO: double check if mutex lock is necessary here. i believe not since this should only be called where mtx is locked
	return time.AfterFunc(entry.normalSocket.retransQueue.rto, func(){entry.retransmitSegment()})
}

// retransmit the segment. called by timer.afterFunc to start the countdown on RTO
func (entry *SocketTableEntry) retransmitSegment() error {
	retransQueue := entry.normalSocket.retransQueue
	retransQueue.mu.Lock()
	defer retransQueue.mu.Unlock()

	// // Bug is fixed, no longer needed
	// /* check for length of queue before doing anything -- cannot retransmit something if queue is empty */
	// if len(retransQueue.array) == 0 {
	// 	fmt.Println("[Retransmit Segment] -- trying to retransmit but queue is empty. Returning.")
	// 	return nil
	// }

	// When RTO timer expires Retransmit earliest unACK’d segment
	segmentToResend := retransQueue.array[0]

	// update to true per RFC 6298 sec 3 (on Karns) to avoid updating RTO on ack of this segment
	segmentToResend.retransmitted = true

	// actually send
	cBuf := entry.normalSocket.sendBuf.cBuf
	sliceToSend := cBuf.SliceFrom(segmentToResend.seqNum,segmentToResend.len)
	fmt.Printf("[TCP - retransmitSegment] re-transmitting head of RQ, contents: [ %s ]\n",string(sliceToSend))
	err := entry.sendSegment(sliceToSend, segmentToResend.seqNum, segmentToResend.flags)
	if err != nil {
		fmt.Println("[TCP - RetransmitSegment] Error sending segment")
		return errors.New("[TCP - RetransmitSegment] bad nested entry.sendSegment call")
	}

	// update RTO
	rto := entry.normalSocket.retransQueue.rto 
	entry.normalSocket.retransQueue.rto = min(rto * 2, RTO_MAX) //  RFC 6298 (5.5):
																// 		The host MUST set RTO <- RTO * 2 ("back off the timer").  The
																//  	maximum value discussed in (2.5) above may be used to provide
																//  	an upper bound to this doubling operation.

	// start the timer again, recursive call
	retransQueue.timer = entry.startRtoTimer()
	//	TODO: pretty sure this is expected behavior for it to spin forever waiting for an ack for a retransmission at RTO_MAX in worst case
	return nil
}

func (retransEntry *RetransmissionEntry) getRtt() time.Duration {
	return time.Since(retransEntry.sent)
}

// TODO: double check there is no issue with the consts all being in milliseconds
// slides formula: SRTT = (⍺ * SRTTLast) + (1 - ⍺)* RTTMeasured
func (retransQueue *RetransmissionQueue) computeNewSrtt(rtt time.Duration) time.Duration {
	if retransQueue.srtt == 0 {
        retransQueue.srtt = rtt
    } else {
        retransQueue.srtt = time.Duration(RTO_ALPHA*float64(retransQueue.srtt) + (1-RTO_ALPHA)*float64(rtt))
    }
	return retransQueue.srtt
}

// TODO: double check there is no issue with the consts all being in milliseconds
// slides formula: RTO = max(RTOMin, min(β * SRTT, RTOMax))
func (retransQueue *RetransmissionQueue) updateRto(rtt time.Duration) error {
	srtt := retransQueue.computeNewSrtt(rtt)
    newRto := time.Duration(RTO_BETA * float64(srtt))
    retransQueue.rto = max(RTO_MIN, min(newRto, RTO_MAX))
	return nil
}



