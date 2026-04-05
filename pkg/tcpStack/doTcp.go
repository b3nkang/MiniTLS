package tcpstack

/*

NEXT STEPS:

ISABELLE 4/4 @7:35PM
	- made preliminary send and recv buf structures
	- wrote method to init them and called before returning from VConnect and VAccept (called in handleSynAck and handleAck)
	- wrote VWRite
	- wrote handleSCommand and REPL stuff to handle and call VWrite (assuming NO circular buffer)

1. set up buffer data structures in Conn after handshake completes
	- simple array for now
	- with pointers
	- plus send/receive threads
2. VWrite and VRead (basic)
	- VWrite: pass bytes through channel to send thread
3. Sending
	- load bytes into buffer
	- send segments
4. Receiving
	- add case for handling basic ACK
	- pass ACK to receiving thread
	- passing information from ACK to sending thread (? or any way to deal with this)

- don't worry about early arrivals initially (just ignore if wrong sequence num)
- refactor handshake methods into separate file

NOTES:
- per RFC: default MSS is 536 (max segment size)

*/

import (
	"errors"
	"fmt"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"

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
	tcpHdr, _, err := utils.ParseAndValidateTCP(hdr, payload) // TODO: re-add tcpData when we need. Its "_" right now because no use, so to turn off compiler warning
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
	
	PrintSocketTableEntry(socketEntry)

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
		if hdr.Flags & header.TCPFlagFin != 0 {
			// TODO: handle for teardowmn
		}
		if hdr.Flags & header.TCPFlagRst != 0 {
			// TODO: handle at some point after mstone2
		}
		// note - may be other flags to handle in this case, to add if so
		if len(payload) < 1 {
			// empty payload with nothing, drop
			return
		}
		socketEntry.handlePayload(tcpHdr, payload) // put into recvBuf and handle all effects
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
func (entry *SocketTableEntry) initBufs(seqNum uint32) {
	sendBuf := &SendBuf{
		buf: make([]byte, MAX_WIN_SIZE),
		currSize: 0,
		dataWrittenToBuf: make(chan struct{}, 1), /* may be better to switch to signal channel and have VWrite write directly to buffer (with mutex) */
	}

	recvBuf := &RecvBuf{
		buf: make([]byte, MAX_WIN_SIZE),
		currSize: 0,
		dataToRead: make(chan []byte), 
	}

	/* init pointers */
	// TODO: all pointer logic below is naive and may need to be revised

	/* send buf */
	sendBuf.nxt = seqNum /* next sequence num to send */
	sendBuf.lbw = seqNum-1 /* last byte written by app, next write starts at lbw+1 */
	sendBuf.base = seqNum

	/* recv buf */
	// recvBuf.lbr = seqNum % MAX_WIN_SIZE  /* i think this is wrong--this treats lbr as an index */
	recvBuf.lbr = seqNum-1 /* last SEQ NUM read by app (start next read from this + 1) */
	recvBuf.base = seqNum
	recvBuf.nxt = recvBuf.lbr + 1 /* next sequence num expected from sender */

	/* idk what this is doing */
	// if recvBuf.lbr == MAX_WIN_SIZE-1 {
	// 	recvBuf.nxt = 0
	// }
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
	// prior logic already handles empty payloads, assume len(payload) > 0
	recvBuf := entry.normalSocket.recvBuf
	payLen := uint32(len(payload))

	// TODO: update, currently we assume this is inorder and not going to wrap
	recvBuf.mu.Lock()
	copy(recvBuf.buf[recvBuf.nxt:recvBuf.nxt+payLen],payload[:payLen])
	recvBuf.currSize += payLen // no check for exceeding bufsize bc sender won't send exceeding since sender-side logic handles
	recvBuf.nxt += payLen
	activeUpdatedSeqNum := tcpHeader.SeqNum+payLen
	recvBuf.mu.Unlock()

    recvBuf.dataToRead <- payload
	entry.sendPureAck(activeUpdatedSeqNum)
	
	return nil
}

// send "pure" ack, i.e. no payload, passive side sends
func (entry *SocketTableEntry) sendPureAck(activeUpdatedSeqNum uint32) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       entry.localPort, // TODO: verify
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		AckNum:        activeUpdatedSeqNum,
		DataOffset:    20,
		Flags:         header.TCPFlagAck,
		WindowSize:    uint16(MAX_WIN_SIZE - entry.normalSocket.recvBuf.currSize),
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
		SrcPort:       entry.localPort, // TODO: verify
		DstPort:       entry.destPort,
		SeqNum:        entry.seqNum,
		AckNum:        entry.lastKnownAck, /* be sure to update this frequently */
		DataOffset:    20,
		Flags:         header.TCPFlagAck, /* data should be type ACK */
		WindowSize:    uint16(MAX_WIN_SIZE - entry.normalSocket.recvBuf.currSize),
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

	return nil
}


/* thread that waits on data in the buffer and sends said data when it's there */
func (entry *SocketTableEntry) sendLoop() {
	conn := entry.normalSocket
	for {
		/* wait for data to be put in buffer by VWrite */
		<-conn.sendBuf.dataWrittenToBuf
		/* lock mutex so nothing changes rn */
		conn.sendBuf.mu.Lock()
		buf := conn.sendBuf

		/* get indices of data to send 
		should be the segment from nxt through lbw */
		start := int(buf.nxt - buf.base) /* next sequence num (to send) - starting sequence num */
		end := int(buf.lbw - buf.base) /* last byte written by app - starting sequence num */

		/* extract data from buffer */
		dataSize := end-start+1
		segmentData := make([]byte, dataSize)
		copy(segmentData, buf.buf[start:end+1])

		conn.sendBuf.mu.Unlock()

		/* if we send without error, move nxt */
		if entry.sendSegment(segmentData) == nil {
			buf.nxt += uint32(dataSize)
		} else {
			fmt.Printf("Error sending segment\n")
		}
	}
}




