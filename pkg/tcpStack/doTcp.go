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
		tcp.handlePayload(socketEntry, tcpHdr, payload) // put into recvBuf and handle all effects
	default:
		fmt.Printf("No known state that matches: %d\n", socketEntry.state)
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
func (tcp *TCPStack) sendTCP(hdr *header.TCPFields, srcIP netip.Addr, destIP netip.Addr, data []byte) {
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
func (conn *VTCPConn) initBufs() {
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

	conn.sendBuf = sendBuf
	conn.recvBuf = recvBuf

	/* start threads for sending and receiving */
}

// ----------- buffer logic ------------

func (tcp *TCPStack) handlePayload(tableEntry *SocketTableEntry, tcpHeader header.TCPFields, payload []byte) error {
	// prior logic already handles empty payloads, assume len(payload) > 0
	recvBuf := tableEntry.normalSocket.recvBuf
	payLen := uint32(len(payload))

	// TODO: update, currently we assume this is inorder and not going to wrap
	recvBuf.mu.Lock()
	copy(recvBuf.buf[recvBuf.nxt:recvBuf.nxt+payLen],payload[:payLen])
	recvBuf.currSize += payLen // no check for exceeding bufsize bc sender won't send exceeding since sender-side logic handles
	recvBuf.nxt += payLen
	activeUpdatedSeqNum := tcpHeader.SeqNum+payLen
	recvBuf.mu.Unlock()

    recvBuf.dataToRead <- payload
	tcp.sendPureAck(tableEntry,activeUpdatedSeqNum)
	
	return nil
}

// send "pure" ack, i.e. no payload, passive side sends
func (tcp *TCPStack) sendPureAck(tableEntry *SocketTableEntry, activeUpdatedSeqNum uint32) error {
	tcpHdr := &header.TCPFields{
		SrcPort:       tableEntry.localPort, // TODO: verify
		DstPort:       tableEntry.destPort,
		SeqNum:        tableEntry.seqNum,
		AckNum:        activeUpdatedSeqNum,
		DataOffset:    20,
		Flags:         header.TCPFlagAck,
		WindowSize:    uint16(MAX_WIN_SIZE - tableEntry.normalSocket.recvBuf.currSize),
		Checksum:      0,
		UrgentPointer: 0,
	}

	/* send using sendTCP */
	tcp.sendTCP(tcpHdr, tableEntry.localIP, tableEntry.destIP, make([]byte, 0))
	return nil
}

