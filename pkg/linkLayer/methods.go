package linklayer

import (
	"fmt"
	"log"
	"net"
	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

const MaxMessageSize = 1400

func ListenLinkLayer(conn net.Conn) error {
	for {
		buffer := make([]byte, MaxMessageSize)

		// Read on the UDP port
		_, sourceAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Panicln("Error reading from UDP socket ", err)
		}

		// Marshal the received byte array into a UDP header
		// NOTE:  This does not validate the checksum or check any fields
		// (You'll need to do this part yourself)
		hdr, err := ipv4header.ParseHeader(buffer)

		if err != nil {
			// What should you if the message fails to parse?
			// Your node should not crash or exit when you get a bad message.
			// Instead, simply drop the packet and return to processing.
			fmt.Println("Error parsing header", err)
			continue
		}

		headerSize := hdr.Len

		// Validate the checksum
		// The checksum is correct if the value we computed matches
		// the value stored in the header.
		// See ValudateChecksum for details.
		headerBytes := buffer[:headerSize]
		checksumFromHeader := uint16(hdr.Checksum)
		computedChecksum := ValidateChecksum(headerBytes, checksumFromHeader)

		var checksumState string
		if computedChecksum == checksumFromHeader {
			checksumState = "OK"
		} else {
			checksumState = "FAIL"
		}

		// Next, get the message, which starts after the header
		message := buffer[headerSize:]

		// Finally, print everything out
		fmt.Printf("Received IP packet from %s\nHeader:  %v\nChecksum:  %s\nMessage:  %s\n",
			sourceAddr.String(), hdr, checksumState, string(message))
	}
}

// Validate the checksum using the netstack package
// Here, we provide both the byte array for the header AND
// the initial checksum value that was stored in the header
//
// "Why don't we need to set the checksum value to 0 first?"
//
// Normally, the checksum is computed with the checksum field
// of the header set to 0.  This library creatively avoids
// this step by instead subtracting the initial value from
// the computed checksum.
// If you use a different language or checksum function, you may
// need to handle this differently.
func ValidateChecksum(b []byte, fromHeader uint16) uint16 {
	checksum := header.Checksum(b, fromHeader)

	return checksum
}