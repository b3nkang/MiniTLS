package protocol

import (
	"fmt"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

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

/* test message handler...idk if there's a better place to put it but nick put it in utils so...*/
func HandleTestMessage(hdr *ipv4header.IPv4Header, payload []byte) {
	message := string(payload)
	fmt.Printf("Received message from %s: %s\n", hdr.Src, message)
}





