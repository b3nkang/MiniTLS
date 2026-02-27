package linkLayer

import (
	"fmt"
	"log"
	"net"
)

const MaxMessageSize = 1400

/* send an IP packet (IPV4 Header + bytes message) via UDP Link Layer */
func (iface *Interface) LinkLayerSend(udpDest *net.UDPAddr, bytes []byte) {
	bytesWritten, err := iface.Conn.WriteToUDP(bytes, udpDest)
	if err != nil {
		log.Panicln("Error writing to socket: ", err)
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
}


/* what to run to constantly listen for new messages on your UDP port */
func (iface *Interface) LinkLayerListen(ipStackChan chan IPPacket) error {
	for {
		buffer := make([]byte, MaxMessageSize)

		/* Read messages from UDP port */
		_, sourceAddr, err := iface.Conn.ReadFromUDP(buffer)
		if err != nil {
			log.Panicln("Error reading from UDP socket ", err)
		}
		
		fmt.Printf("[LL] Received IP packet from %s. Forwarding to IP Stack...\n", sourceAddr.String())

		packet := IPPacket{
			SrcIfaceAddr: sourceAddr,
			Data: buffer,
		}

		ipStackChan <- packet
	}
}

// /* Checksum field initially set to 0 */
// func ValidateChecksum(b []byte, fromHeader uint16) uint16 {
// 	checksum := header.Checksum(b, fromHeader)

// 	return checksum
// }

// /* Compute the checksum using the netstack package */
// func ComputeChecksum(b []byte) uint16 {
// 	checksum := header.Checksum(b, 0)

// 	/* Invert the checksum value.  Why is this necessary?
// 	This function returns the inverse of the checksum
// 	on an initial computation.  While this may seem weird,
// 	it makes it easier to use this same function
// 	to validate the checksum on the receiving side.
// 	See ValidateChecksum in the receiver file for details. */
// 	checksumInv := checksum ^ 0xffff

// 	return checksumInv
// }

// /* Just a simple data structure for an IP Packet (header and message) */
// type IPPacket struct {
// 	Header *ipv4header.IPv4Header
// 	Data []byte /* message field */
// }


/*
Imported: 

var (
	errInvalidConn       = errors.New("invalid connection")
	errMissingAddress    = errors.New("missing address")
	errNilHeader         = errors.New("nil header")
	errHeaderTooShort    = errors.New("header too short")
	errExtHeaderTooShort = errors.New("extension header too short")
	errInvalidConnType   = errors.New("invalid conn type")
)

// A Header represents an IPv4 header.
type IPv4Header struct {
	Version  int         // protocol version
	Len      int         // header length
	TOS      int         // type-of-service
	TotalLen int         // packet total length
	ID       int         // identification
	Flags    HeaderFlags // flags
	FragOff  int         // fragment offset
	TTL      int         // time-to-live
	Protocol int         // next protocol
	Checksum int         // checksum
	Src      netip.Addr  // source address
	Dst      netip.Addr  // destination address
	Options  []byte      // options, extension headers
}



*/