package tcpstack

/*

NEXT STEPS:
- send SYN method
	- and VConnect
- finish 3-way handshake:
	- receive SYN-ACK
	- send ACK
	- receive ACK and send Conn to listener chan so that Accept can return

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

	/* 1. parse TCP header and extract message body */
	tcpHdr, tcpData, err := utils.ParseAndValidateTCP(hdr, payload)
	if err != nil {
		/* checksum failed */
		fmt.Println("Error: %s\n", err.Error())
	}

	/* 2. match tuple to our table; tableMatch(srcPort, srcIP, destPort, destIP) */
	socketEntry, err := table.tableMatch(tcpHdr.SrcPort, hdr.Src, tcpHdr.DstPort, hdr.Dst)
	if err != nil {
		/* didn't find match */
		fmt.Println("[TCP] No normal or listener socket open that matches the following:\n")
		fmt.Printf("Src Port: %s\nSrc IP; %s\n Dest Port: %s\n Dest IP: %s\n", string(tcpHdr.SrcPort), hdr.Src.String(), string(tcpHdr.DstPort), hdr.Dst.String())
	} 

	/* 3. act differently based on state of that conn in our table */
	switch socketEntry.state {
	case LISTEN:
		/* we matched the listen socket--so we should be getting an initial SYN */
		/* should pass in IP Source as OUR DEST and IP Dest as OUR SOURCE since this is FROM REMOTE */
		tcp.handleSyn(socketEntry.listenSocket, tcpHdr, tcpData, hdr.Dst, hdr.Src)
		return
	case SYN_RECEIVED:
		/* if we are in this state, we should have gotten SYN-ACK */
		//tcp.handleSynAck(socketEntry.normalSocket, tcpHdr, tcpData)
	default:
		fmt.Println("No known state that matches: %d", socketEntry.state)
	}

}

/* What we do when we get a match on a listen socket
(if listen socket is accepting)
	1. Make normal conn
	2. Add to socket table with SYN_RECEIVED state
	3. send SYN-ACK
*/
func (tcp *TCPStack) handleSyn(listener *VTCPListener, tcpHeader header.TCPFields, data []byte, localIP netip.Addr, destIP netip.Addr) {
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
	defer table.mu.Unlock()

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
		socketID: table.nextID,
		seqNum: utils.GenerateNewSeq(), /* generate random sequence number here for starting */
	}

	table.nextID++ /* need to increment for next connection */
	table.socketMap[entry.socketID] = entry

	/* TODO: send SYN-ACK */
	tcp.sendSynAck(entry, tcpHeader.SeqNum)
}

/* send SYN-ACK (second part of 3-way handshake) */
func (tcp *TCPStack) sendSynAck(tableEntry *SocketTableEntry, synSeq uint32) error {
	/* make TCP header */
	tcpHdr := &header.TCPFields{
		SrcPort:       tableEntry.localPort,
		DstPort:       tableEntry.destPort,
		SeqNum:        tableEntry.seqNum,
		AckNum:        synSeq+1, 	/* Ack should be whatever we got from SYN + 1 */
		DataOffset:    20, 			/* TODO: I have no idea what this is */
		Flags:         header.TCPFlagSyn | header.TCPFlagAck,
		WindowSize:    65535, 		/* TODO: figure out what this actually should be */
		Checksum:      0,
		UrgentPointer: 0,
	}

	/* send using sendTCP */
	tcp.sendTCP(tcpHdr, tableEntry.localIP, tableEntry.destIP, make([]byte, 0))
	return nil

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

	/* TODO: return bytes written/sent? */
}