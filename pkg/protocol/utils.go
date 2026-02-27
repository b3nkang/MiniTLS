package protocol

import "github.com/google/netstack/tcpip/header"

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





