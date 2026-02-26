package linkLayer

import (
	"net"
	"net/netip"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
)

/*
	Represents 1 local subnet
	Neighbors: reachable via UDP (link layer) through this interface
*/
type Interface struct {
	Name  		string								/* ex: "if0" */
	Prefix		netip.Prefix 						/* 10.2.0.1/24 */
	IP 			netip.Addr
	Conn  		*net.UDPConn  							/* opened UDP socket */
	Neighbours 	map[netip.Addr]Neighbour
}

/*
	For representing neighbors existing on the same local network
*/
type Neighbour struct {
	IP  		netip.Addr 			// virtual IP (10.2.0.3)
	UDPAddr		netip.AddrPort   	/* convert to net.UDPAddr when needed */
}

/* Just a simple data structure for an IP Packet (header and message) */
type IPPacket struct {
	Header *ipv4header.IPv4Header
	Data []byte /* message field */
}

