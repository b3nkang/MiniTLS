package linkLayer

import (
	"fmt"
	"log"
	"net"
	"net/netip"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

const MaxMessageSize = 1400

/* send an IP packet (IPV4 Header + bytes message) via UDP Link Layer */
func LinkLayerSend(conn *net.UDPConn, udpDest *net.UDPAddr, source netip.Addr, dest netip.Addr, message string, protocol int) {
	/* Start filling in the header, use passed in fields */
	hdr := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(message),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      32, /* will need to update */
		Protocol: protocol,
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      source,
		Dst:      dest,
		Options:  []byte{},
	}

	// Assemble the header into a byte array
	headerBytes, err := hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
	}

	// Compute the checksum (see below)
	// Cast back to an int, which is what the Header structure expects
	hdr.Checksum = int(ComputeChecksum(headerBytes))

	headerBytes, err = hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
	}

	/* Append header + message into one byte array */
	bytesToSend := make([]byte, 0, len(headerBytes)+len(message))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, []byte(message)...)

	// Send the message to the "link-layer" addr:port on UDP
	// FOr h1:  send to port 5002
	// ONE CALL TO WriteToUDP => 1 PACKET
	bytesWritten, err := conn.WriteToUDP(bytesToSend, udpDest)
	if err != nil {
		log.Panicln("Error writing to socket: ", err)
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
}


/* what to run to constantly listen for new messages on your UDP port */
func LinkLayerListen(conn *net.UDPConn, ipStackChan chan IPPacket) error {
	for {
		buffer := make([]byte, MaxMessageSize)

		/* Read messages from UDP port */
		_, sourceAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Panicln("Error reading from UDP socket ", err)
		}

		/* Marshal the received byte array into a UDP header
		NOTE:  This does not validate the checksum or check any fields */
		hdr, err := ipv4header.ParseHeader(buffer)

		if err != nil {
			/* drop packet if parsing doesn't work */
			fmt.Println("Error parsing header", err)
			continue
		}

		/* extract and validate checksum */
		headerSize := hdr.Len
		headerBytes := buffer[:headerSize]
		checksumFromHeader := uint16(hdr.Checksum)
		computedChecksum := ValidateChecksum(headerBytes, checksumFromHeader)

		/* determine if we passed or failed checksum */
		if computedChecksum == checksumFromHeader {
			fmt.Println("Checksum passed")
		} else {
			fmt.Printf("Checksum failed, dropping packet from %s\n", sourceAddr.String())
			continue // drop the packet just by continuing
		}

		/* Next, get the message, which starts after the header */
		message := buffer[headerSize:]

		/* print out all the stuff */
		fmt.Printf("Received IP packet from %s\nHeader:  %v\nChecksum:  PASSED\nMessage:  %s\n",
			sourceAddr.String(), hdr, string(message))
		/* build an IPPacket and send to IPStack */
		packet := IPPacket{
			Header: hdr,
			Data: message,
		}
		ipStackChan <- packet
	}
}

/* Checksum field initially set to 0 */
func ValidateChecksum(b []byte, fromHeader uint16) uint16 {
	checksum := header.Checksum(b, fromHeader)

	return checksum
}

/* Compute the checksum using the netstack package */
func ComputeChecksum(b []byte) uint16 {
	checksum := header.Checksum(b, 0)

	/* Invert the checksum value.  Why is this necessary?
	This function returns the inverse of the checksum
	on an initial computation.  While this may seem weird,
	it makes it easier to use this same function
	to validate the checksum on the receiving side.
	See ValidateChecksum in the receiver file for details. */
	checksumInv := checksum ^ 0xffff

	return checksumInv
}

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