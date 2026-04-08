package tcpstack

/*

NEXT STEPS:

// TODO: refactor tcpHeader = SEGheader or something to make things much less confusing

Things Isabele Did Today (4/6 before 5:00pm):
	- got send working with correct indices (fixed InitBuf method)
		- some other small bug fixes to do this but I lowkey forgot--mostly indexing
			stuff and accessing the buffer
		- good thing to remember: pointers in the buffer refer to SEQUENCE NUMS, not indices.
			to get indices (for now) subtract the base pointer (eventually will change due to circular
				buffer but we'll write methods to do that)
	- switched the channel in Receive buffer to be a signal channel, not data
		- because HandlePayload was blocking on sending received data to the data channel
				but if no one had called VRead, we don't want them to block
		- so now VRead will have to adjust pointers in the buffer (just like VWrite) and
				take on a bit more responsibility
	- send is now working repeatedly and from both sides
		- sequence nums from send side should be updating correctly (didn't look in wireshark tho)
		- things in recv buf should be updating correctly too
	- VRead was very wrong...needed a loop (so that it blocks until data is ready)
		- also needs to check CurrSize and not just signal--signal is only for NEW data but if
				it just wants to read old data, then it doesn't need a signal to look

Next steps after what Isabelle did today:

4. Receiving
	- I think Acks are being sent but not sure if they're correct--verify that
		- write method to handle ack and then do the things below
	- passing information from ACK to sending thread
		- sending thread needs to know window size and keep track of data IN FLIGHT and
			compare those numbers before sending new data
		- we aren't doing this at all yet
5. Circular buffer
	- Make a circular buffer struct that has methods for indexing, etc and add to send & recv buf

- don't worry about early arrivals initially (just ignore if wrong sequence num)

NOTES:
- per RFC: default MSS is 536 (max segment size) // TODO: i'm not enforcing this yet, should we? -ben

// TODO: small, refactor tcpHeader = SEGheader or something to make things much less confusing
// TODO: low prio/long term, look into if seqNum field is even necessary in table entry. may not be

*/

import (
	"errors"
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
	"strings"

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
		case tcpHdr.Flags & header.TCPFlagFin != 0:
			// TODO: teardown
			return
		case tcpHdr.Flags&header.TCPFlagRst != 0:
			// TODO: handle later
			return
		case tcpHdr.Flags & header.TCPFlagAck != 0 && len(tcpPayload) == 0:
			fmt.Println("[TCP - HandleTCP] recvd pureAck, handling")
			socketEntry.handlePureAck(tcpHdr)
			return
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
	// sendBuf := &SendBuf{
	// 	buf: make([]byte, MAX_WIN_SIZE),
	// 	currSize: 0,
	// 	dataWrittenToBuf: make(chan struct{}, 1), /* may be better to switch to signal channel and have VWrite write directly to buffer (with mutex) */
	// }

	// recvBuf := &RecvBuf{
	// 	buf: make([]byte, MAX_WIN_SIZE),
	// 	currSize: 0,
	// 	dataToRead: make(chan struct{}, 1), 
	// }

	sendCBuf := NewCircleBuf(MAX_WIN_SIZE, entry.seqNum)
	sendBuf := &SendBuf{
		cBuf: sendCBuf,
		dataWrittenToBuf: make(chan struct{}, 1),
	}
	recvCBuf := NewCircleBuf(MAX_WIN_SIZE, otherSideSeq)
	recvBuf := &RecvBuf{
		cBuf: recvCBuf,
		dataToRead: make(chan struct{}, 1), 
	}

	/* init pointers */
	// TODO: all pointer logic below is naive and may need to be revised
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
	payLen := uint32(len(payload))

	// TODO: update, currently we assume this is inorder and not going to wrap
	recvBuf.mu.Lock()

	/* check that seq num is what we expect (TODO: deal with early arrivals) */
	// TODO: also deal with OLD/redundant segments - technically we are supposed to discard payload and send an ack back
	if tcpHeader.SeqNum != recvBuf.nxt {
		fmt.Printf("Expected SeqNum: %d, Actual SeqNum: %d, dropping payload\n", int(recvBuf.nxt), int(tcpHeader.SeqNum))
		recvBuf.mu.Unlock()
		return nil
	}

	// // OLD -- pre circbuf

	// /* Now assume seqNum is what we expect: 
	// 	convert seq nums to indices for copy */
	// startCopy := recvBuf.nxt - recvBuf.base
	// endCopy := startCopy + payLen

	/* TODO: check if there is space in buffer ----> replied below -Ben */
	// note: as below, no check for exceeding bufsize bc sender won't send exceeding since sender-side logic handles with otherSideWindow

	// copy(recvBuf.buf[startCopy:endCopy],payload[:payLen])
	// recvBuf.currSize += payLen // no check for exceeding bufsize bc sender won't send exceeding since sender-side logic handles


	bytesWritten := recvBuf.cBuf.WriteIntoBuf(recvBuf.nxt, payload)
	if bytesWritten != len(payload) {
		fmt.Printf("[TCP - handlePayload] only wrote %d/%d bytes into recv buffer\n",bytesWritten,len(payload))
		recvBuf.mu.Unlock()
		return nil
	}
	
	recvBuf.nxt += payLen	/* next updated here! don't move lbr until READ() */
	activeUpdatedSeqNum := tcpHeader.SeqNum+payLen
	fmt.Printf("state: base=%d nxt=%d lbr=%d currSize=%d payLen=%d startCopy=%d\n", recvBuf.cBuf.baseSeq, recvBuf.nxt, recvBuf.lbr, recvBuf.cBuf.currSize, payLen)

	/* print receive buffer for debugging */
	fmt.Printf("copied bytes: %q\n", recvBuf.cBuf.SliceFrom(tcpHeader.SeqNum, payLen))
	if recvBuf.cBuf.currSize > 0 {
		fmt.Printf("entire readable region: %q\n",recvBuf.cBuf.SliceFrom(recvBuf.lbr+1, recvBuf.cBuf.currSize))
	} else {
		fmt.Printf("entire readable region: %q\n", []byte{})
	}

	recvBuf.mu.Unlock()

    select {
	case recvBuf.dataToRead <- struct{}{}:
	default:
	}
	entry.sendPureAck(activeUpdatedSeqNum)
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
		WindowSize:    uint16(MAX_WIN_SIZE - entry.normalSocket.recvBuf.cBuf.currSize),
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
		AckNum:        entry.lastKnownAck, /* be sure to update this frequently */
		DataOffset:    20,
		Flags:         header.TCPFlagAck, /* data should be type ACK */
		WindowSize:    uint16(MAX_WIN_SIZE - entry.normalSocket.recvBuf.cBuf.currSize),
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
func (entry *SocketTableEntry) handlePureAck(seg header.TCPFields) error {
	sendBuf := entry.normalSocket.sendBuf
	sendBuf.mu.Lock()
	defer sendBuf.mu.Unlock()

	// ------------------  TODO: test edge cases once circular array is up --------------

	// RFC: SND.UNA < SEG.ACK <= SND.NXT
	if sendBuf.una < seg.AckNum && seg.AckNum <= sendBuf.nxt {
		fmt.Println("[TCP - handlePureAck] adjusting UNA to seg.AckNum")
		ackedBytes := seg.AckNum - sendBuf.una
		sendBuf.una = seg.AckNum
		sendBuf.cBuf.AdvanceBase(ackedBytes)
	} else if seg.AckNum > sendBuf.nxt { // RFC: If the ACK acks something not yet sent (SEG.ACK > SND.NXT), then send an ACK, drop the segment, and return
		// TODO: implement a sendPureAckForSender(). this is a bit of a pain, since sendPureAck() is for the recv side and thus we need a new version
		fmt.Println("[TCP - handlePureAck] TODO, condition seg.AckNum > sendBuf.nxt, no fix implemented yet")
		return nil
	} else if seg.AckNum <= sendBuf.una { // RFC: If the ACK is a duplicate (SEG.ACK =< SND.UNA), it can be ignored.
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

		/* lock mutex so nothing changes rn */
		conn.sendBuf.mu.Lock()
		sendBuf := conn.sendBuf

		// if not necessary, pass
		if sendBuf.lbw < sendBuf.nxt {
			fmt.Println("[TCP - sendLoop] no unsent data")
			conn.sendBuf.mu.Unlock()
			continue
		}

		// // old pre-circlebuf 
		// /* get indices of data to send 
		// should be the segment from nxt through lbw */
		// nxt := int(sendBuf.nxt - sendBuf.base) /* next sequence num (to send) - starting sequence num */
		// lbw := int(sendBuf.lbw - sendBuf.base) /* last byte written by app - starting sequence num */
		// // if no unsent data, just return
		// if lbw < nxt {
		// 	fmt.Println("no unsent data")
		// 	conn.sendBuf.mu.Unlock()
		// 	return
		// }

		// /* print send buffer for debugging */
		// fmt.Println("Printing send buffer contents copied before sending segment: ")
		// fmt.Printf("copied bytes: %q\n", sendBuf.buf[nxt:lbw+1])
		// fmt.Printf("entire buffer: %q\n", sendBuf.buf[:sendBuf.currSize])

		fmt.Println("Printing send buffer contents copied before sending segment:")
		bytesAvailableInBuf := sendBuf.lbw-sendBuf.nxt+1 	// NOTE: lbw is INCLUSIVE, meaning that we MUST add +1
															// 		 since LBW points to a byte that we need to send also
		fmt.Printf("[TCP - sendLoop] state: base=%d una=%d nxt=%d lbw=%d currSize=%d bytesAvail=%d otherSideWindow=%d\n",
					sendBuf.cBuf.baseSeq,sendBuf.una,sendBuf.nxt,sendBuf.lbw,sendBuf.cBuf.currSize,bytesAvailableInBuf,sendBuf.otherSideWindow)
		/* whole send buffer */
		if sendBuf.cBuf.currSize > 0 {
			fmt.Printf("entire live send buffer: %q\n",sendBuf.cBuf.SliceFrom(sendBuf.cBuf.baseSeq, sendBuf.cBuf.currSize))
		} else {
			fmt.Printf("entire live send buffer: %q\n", []byte{})
		}

		/* TODO: start zero-window-probing if necessary */

		// CHECK HOW MUCH WE CAN SEND: min(SND.LBW - SND.NXT, SND.WND - (SND.NXT-SND.UNA))
		// 			where SND.LBW - SND.NXT is just everything in the buffer to-be-sent
		//			where SND.WND - (SND.NXT-SND.UNA) is otherSideWindow - bytesInFlight

		windowRemaining := int(sendBuf.otherSideWindow) - int(sendBuf.getBytesInFlight())
		if windowRemaining <= 0 { // TODO: add ZWP here i think? should be here
			fmt.Println("[TCP - sendloop] no window left") // update: couple hours later, ran into ZWP issue here. TODO:!
			conn.sendBuf.mu.Unlock()
			continue
		}

		var maxBytesSendable uint32
		if int(bytesAvailableInBuf) <= windowRemaining {
			maxBytesSendable = bytesAvailableInBuf
			fmt.Println("[TCP - sendLoop] copying all available bytes in sendBuf")
		} else {
			maxBytesSendable = uint32(windowRemaining)
			fmt.Println("[TCP - sendLoop] copying only remaining receiver window")
		}

		// segmentData := make([]byte, maxBytesSendable)
		// if copyAllAvailableBytes {
		// 	copy(segmentData, sendBuf.buf[nxt:lbw+1])
		// 	fmt.Println("[TCP - sendloop] copying all avail bytes in sendBuf")
		// } else {
		// 	copy(segmentData, sendBuf.buf[nxt:nxt+maxBytesSendable])
		// 	fmt.Println("[TCP - sendloop] copying only the remWindow - bytesInFlight into sendBuf")
		// }

		segmentData := sendBuf.cBuf.SliceFrom(sendBuf.nxt, uint32(maxBytesSendable))
		conn.sendBuf.mu.Unlock()

		/* if we send without error, move nxt */
		if entry.sendSegment(segmentData) == nil {
			conn.sendBuf.mu.Lock()
			sendBuf.nxt += uint32(maxBytesSendable)
			entry.seqNum = sendBuf.nxt // move seqNum update here so it does not diverge from snd.nxt in case of err
			conn.sendBuf.mu.Unlock()
		} else {
			fmt.Printf("Error sending segment\n")
		}

		// // Only a facility for non-circ arrays
		// // viz after all updates
		// fmt.Printf("> ")
		// printBufferWithPointers(sendBuf.cBuf.buf, sendBuf.base, 10, []BufPointer{
		// 	{seq: sendBuf.una, mark: "U"},
		// 	{seq: sendBuf.nxt, mark: "N"},
		// 	{seq: sendBuf.lbw, mark: "L"},
		// })
	}
}

// tiny helper
func (sendBuf *SendBuf) getBytesInFlight() uint32 {
	return sendBuf.nxt - sendBuf.una
}

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