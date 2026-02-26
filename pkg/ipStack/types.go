package ipStack

import (
	"net"
	"net/netip"
)

/*
	Contains:
	- map "ifo" : Interface{"ifo", 10.0.0.0/24, net.Conn(Link Layer), neighbors on that local net }
	- List of fwding table entries to iterate through
*/
type IPStack struct {
	Interfaces			map[string]*Interface  	/* “if0” : Interface() */
	ForwardingTable 	map[netip.Prefix]FwdEntry
	IncomingPackets 	chan IPPacket
}

/*
	For representing neighbors existing on the same local network
*/
type Neighbour struct {
	IP  		netip.Addr 			// virtual IP (10.2.0.3)
	UDPAddr		netip.AddrPort   	/* convert to net.UDPAddr when needed */
}

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
	For entries in Routers' Forwarding Table
	- Direct = directly connected via local interface
	- RIP = learned about through RIP--routed through multiple hops
*/
const (
	SourceTypeDirect = 0
	SourceTypeRIP = 1
)

/* 
	Represents one entry in hosts'/routers' forwarding table
	Use InterfaceName after prefix matching to access IPStack's Interfaces list
*/
type FwdEntry struct {
	Prefix			netip.Prefix		/* ex: 10.2.0.1/24 */
	NextHop   		netip.Addr		/* zero if directly connected */
	InterfaceName 	string   		/* ex: "ifo" */
	Type	   		int				/* Direct or RIP (routers) */
	Cost 			int 			/* for RIP */
}

/*
Will also need a data structure for RIP to accommodate these fields in Config:

routing rip

# Neighbor routers that should be sent RIP messages
rip advertise-to 10.1.0.2

# Timing parameters for RIP
rip periodic-update-rate 5000 # in milliseconds
rip route-timeout-threshold 12000 # in milliseconds


*/

