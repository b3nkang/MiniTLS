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
	Up 			bool								/* is interface up */
}

/*
	For representing neighbors existing on the same local network
*/
type Neighbour struct {
	IP  		netip.Addr 			// virtual IP (10.2.0.3)
	UDPAddr		netip.AddrPort   	/* convert to net.UDPAddr when needed */
}
