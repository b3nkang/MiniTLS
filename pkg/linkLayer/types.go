package linkLayer

import (
	"net"
	"net/netip"
)

/*
	Represents 1 local subnet
	Neighbors: reachable via UDP (link layer) through this interface
*/
type Interface struct {
	Name  		string								/* ex: "if0" */
	Prefix		netip.Prefix 						/* 10.2.0.1/24 */
	IP 			netip.Addr
	Conn  		*net.UDPConn  						/* opened UDP socket */
	Neighbours 	map[netip.Addr]*Neighbour			/* neighbour IP addr : Neighbour() */
}

/*
	For representing neighbors existing on the same local network
*/
type Neighbour struct {
	IP  		netip.Addr 			// virtual IP (10.2.0.3)
	UDPAddr		netip.AddrPort   	/* convert to net.UDPAddr when needed */
}

/* Just a simple data structure for an IP Packet (header and message) */
type IPPacket struct { // TODO: type is very likely not necessary and can be converted into just IncomingPacketChan []byte 

	SrcIfaceAddr *net.UDPAddr 	/* the interface this packet came in on. TODO: verify if this is even
								needed, if not, change IPStack IncomingPacketChan to just []byte type */
	Data []byte /* message field */
}

