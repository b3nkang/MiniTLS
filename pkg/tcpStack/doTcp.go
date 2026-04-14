package tcpstack

/*

Recent changes:

4. Receiving ---------------------------------------------------------------------------------------> done. did a decent amount of testing, decently confident
    - I think Acks are being sent but not sure if they're correct--verify that ---------------------> they are now and should be correct
        - write method to handle ack and then do the things below ----------------------------------> done
    - passing information from ACK to sending thread -----------------------------------------------> done
        - sending thread needs to know window size and keep track of data IN FLIGHT and
            compare those numbers before sending new data ------------------------------------------> done
        - we aren't doing this at all yet
5. Circular buffer ---------------------------------------------------------------------------------> it is implemented with light testing indicating it's working.
    - Make a circular buffer struct that has methods for indexing, etc and add to send & recv buf     more testing is needed however. you can test edge cases by
                                                                                                      changing MAX_WIN_SIZE in types.go from 65k to like 15

NEXT STEPS/TODO:s:
    - check that the circleBuf slop is looking ok -- seems fine?
        - I did get to a point where I ran into a ZWP crash, so ZWP likely needs to be implemented to make progress on this -- next milestone
        - My wireshark must be broken bc I can't see any packets, so the testing was all through prints so impl could be a bit sus,
          so if you can do this I would recommend checking there --done, looks good
    - handle early arrivals. i don't actually think this should be too bad at all, min heap plus an extra check in HandlePayload()
    - much further down the line but from here I think it's beginning sf and rf repl commands?

NOTES:
- per RFC: default MSS is 536 (max segment size) // TODO: i'm not enforcing this yet, should we? -ben
	- NO--doesn't have to be 536, but does have to adhere to this:
	As in the IP assignment, never send packets greater than the MTU.
	For our link layer, the maximum MTU is 1400 bytes:
	any TCP segments you send must be no larger than the MTU–therefore,
	the maximum TCP payload size is: 1400 bytes - (size of IP header) - (size of TCP header)

	This means  MSS is 1360 i think? maybe we should verify this...Verified: we can set MSS to whatever we want!

// TODO: small, refactor tcpHeader = SEGheader or something to make things much less confusing
// TODO: low prio/long term, look into if seqNum field is even necessary in table entry. may not be

*/

import (
	"errors"
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
	"strings"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

/* handler function that receives all TCP messages
not sure if we need the IP header here but will leave for now
*/
func (tcp *TCPStack) HandleTCP(hdr *ipv4header.IPv4Header, payload []byte) {
	table := tcp.socketTable

	fmt.Println("[TCP] HandleTCP called, parsing and validating TCP message")

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
	} 
	
	// PrintSocketTableEntry(socketEntry)

	/* 3. act differently based on state of that conn in our table */
	switch socketEntry.state {
	case LISTEN:
		/* we matched the listen socket--so we should be getting an initial SYN */
		/* should pass in IP Source as OUR DEST and IP Dest as OUR SOURCE since this is FROM REMOTE */
		fmt.Println("[TCP] Packet sent to listen socket--starting 3-way handshake")
		tcp.handleSyn(socketEntry.listenSocket, tcpHdr, hdr.Dst, hdr.Src)
		return
	case SYN_RECEIVED:
		fmt.Println("[TCP] handler received packet in state SYN-RECEIVED -> handling ACK")
		tcp.handleAckHandshake(socketEntry, tcpHdr)
	case SYN_SENT:
		fmt.Println("[TCP] handler received packet in state SYN-SENT -> handling SYN-ACK")
		tcp.handleSynAck(socketEntry, tcpHdr)
	case ESTABLISHED:
		fmt.Println("[TCP] handler received packet in state ESTABLISHED -> handling flags and/or payload")

		switch {
		/* FIN specified */
		case tcpHdr.Flags & header.TCPFlagFin != 0:
			// TODO: teardown
			return
		/* Reset specified -- immediately abort connection */
		case tcpHdr.Flags&header.TCPFlagRst != 0:
			// TODO: handle later
			return
		/* data ACK */
		case tcpHdr.Flags & header.TCPFlagAck != 0 && len(tcpPayload) == 0:
			fmt.Println("[TCP - HandleTCP] recvd pureAck, handling")
			socketEntry.handlePureAck(tcpHdr)
			return
		/* invalid/empty packet */
		case len(tcpPayload) < 1:
			fmt.Println("[TCP - HandleTCP] recvd full empty packet, dropping")
			return
		}
		// note - may be other flags to handle in this case, to add if so
		socketEntry.handlePayload(tcpHdr, tcpPayload) // put into recvBuf and handle all effects
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
	fmt.Printf("[TCP] SendTCP sent message from SRC: %s to DST: %s\n", srcIP.String(), destIP.String())

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
	fmt.Println("Packet being handled by receiver in handlePayload")
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

	/* quick space check before writing -- shouldn't be neccesary if sender works right */
	space := int(recvBuf.cBuf.FreeSpace())
	if space == 0 {
		fmt.Println("[TCP - handlePayload] Space in recv buffer is 0--should not be sending this packet")
		recvBuf.mu.Unlock()
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
		/* nothing in heap or min num is not our nxt pointer */
		if min == nil || min.startSeq != recvBuf.nxt {
			break
		}
		/* if not enough space in the recv buffer for full segment, just abandon ship 
			TODO--verify if that is okay 
			- this means we will advertise win=X where X < Max Segment Size
			- sender will send another segment of that size and then ZWP will start so we're good */
		space := int(recvBuf.cBuf.FreeSpace())
		if len(min.data) > space {
			break
		}

		/* actually take out minimum segment and write to recv buffer */
		segment := recvBuf.earlyArrivals.PopMin()
		numBytesWritten := recvBuf.cBuf.WriteIntoBuf(segment.startSeq, segment.data)
		recvBuf.nxt += uint32(numBytesWritten)
	}

	/* ------ print receive buffer for debugging ----- */
	fmt.Printf("state: base=%d nxt=%d lbr=%d currSize=%d payLen=%d\n", recvBuf.cBuf.baseSeq, recvBuf.nxt, recvBuf.lbr, recvBuf.cBuf.currSize, payLen)
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
func (entry *SocketTableEntry) sendSegment(segment []byte) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort,
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		AckNum:        entry.normalSocket.recvBuf.nxt,
		DataOffset:    20,
		Flags:         header.TCPFlagAck, /* data should be type ACK */
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

	// RFC: SND.UNA < SEG.ACK <= SND.NXT -> ACK num of segment is less than or equal to our next Sequence Num (in our send buf)
	if sendBuf.una < seg.AckNum && seg.AckNum <= sendBuf.nxt {
		fmt.Println("[TCP - handlePureAck] adjusting UNA to seg.AckNum")
		ackedBytes := seg.AckNum - sendBuf.una /* num bytes accounted for via this ACK */
		sendBuf.una = seg.AckNum /* move UNA up */
		/* adjust internals of circular buffer to reflect num bytes Acked */
		sendBuf.cBuf.AdvanceBase(ackedBytes)
		/* tell sendBuf that there is space */	
		select {
		case sendBuf.spaceAvailable <- struct{}{}:
		default:
		}
	} else if seg.AckNum > sendBuf.nxt { // RFC: If the ACK acks something not yet sent (SEG.ACK > SND.NXT), then send an ACK, drop the segment, and return
		// TODO: implement a sendPureAckForSender(). this is a bit of a pain, since sendPureAck() is for the recv side and thus we need a new version
		fmt.Println("[TCP - handlePureAck] TODO, condition seg.AckNum > sendBuf.nxt, no fix implemented yet")
		return nil
	} else if seg.AckNum <= sendBuf.una { // RFC: If the ACK is a duplicate (SEG.ACK =< SND.UNA), it can be ignored.
		// we also don't need to update retransQueue since the out-of-order segment will have sliced this segment off already
		return nil
	} else {
		fmt.Println("[TCP - handlePureAck] condition should not have been hit")
		return nil
	}
	sendBuf.otherSideWindow = seg.WindowSize // update new window size
	fmt.Printf("[TCP - handlePureAck] sender got new recvr window size: %d\n", sendBuf.otherSideWindow)
	return nil
}

/* thread that waits on data in the buffer and sends said data when it's there 
   TODO: verify there is enough space in the buffer to send. this will rely
   on some sort of field/data structure that DOES NOT EXIST YET -> need to 
   know other side's window size and update it with Acks */
func (entry *SocketTableEntry) sendLoop() {
	conn := entry.normalSocket
	for {
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

			if windowRemaining <= 0 {
				fmt.Println("[TCP - sendloop] no window left") // update: couple hours later, ran into ZWP issue here. TODO:!
				/* TODO: start zero-window-probing here! */
				conn.sendBuf.mu.Unlock()
				break
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

			if entry.sendSegment(segmentData) != nil {
				fmt.Println("[TCP - Send Loop] Error sending segment")
				break
			}

			// add to retransmission queue
			segmentRetransEntry := &RetransmissionEntry{
				seqNum: entry.seqNum,
				len: uint32(maxBytesSendable),
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

	// When RTO timer expires Retransmit earliest unACK’d segment
	segmentToResend := retransQueue.array[0]

	// update to true per RFC 6298 sec 3 (on Karns) to avoid updating RTO on ack of this segment
	segmentToResend.retransmitted = true

	// actually send
	cBuf := entry.normalSocket.sendBuf.cBuf
	err := entry.sendSegment(cBuf.SliceFrom(segmentToResend.seqNum,segmentToResend.len))
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
	retransQueue.rto = time.Duration(max(RTO_MIN, min(RTO_BETA*float64(retransQueue.computeNewSrtt(rtt)),RTO_MAX)))
	return nil
}

// TODO: delete these functions and the repl command associated
/* ----------------------PRINTING (NON-CIRCL BUF, OBSOLETE NOW------------------*/
type BufPointer struct {
	seq  uint32
	mark string
}

func printBufferWithPointers(buf []byte, base uint32, upTo int, pointers []BufPointer) {
	if upTo <= 0 {
		fmt.Println("[printPointers] upTo must be > 0")
		return
	}
	if upTo > len(buf) {
		upTo = len(buf)
	}

	vals := make([]string, upTo)
	for i := 0; i < upTo; i++ {
		if buf[i] == 0 {
			vals[i] = padCell(".")
		} else {
			vals[i] = padCell(string(buf[i]))
		}
	}

	marks := make([]string, upTo)
	for i := 0; i < upTo; i++ {
		marks[i] = padCell(".")
	}

	for _, ptr := range pointers {
		idx := int(ptr.seq - base)
		if idx < 0 || idx >= upTo {
			continue
		}

		curr := trimCell(marks[idx])
		if curr == "." {
			curr = ptr.mark
		} else {
			curr += ptr.mark
		}
		marks[idx] = padCell(curr)
	}

	printRow("", vals)
	printRow("  ", marks)
}

func printRow(prefix string, row []string) {
	fmt.Print(prefix)
	fmt.Print("[")
	for i, s := range row {
		fmt.Print(s)
		if i != len(row)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Println("]")
}

func padCell(s string) string {
	if len(s) >= 3 {
		return s
	}
	for len(s) < 3 {
		s += " "
	}
	return s
}

func trimCell(s string) string {
	return strings.TrimSpace(s)
}