package ipstack

import (
	"net"
	"net/netip"
)

type IPStack struct {
	Interfaces			map[string]*Interface  	// “if0” : Interface()
	ForwardingTable 	[]FwdEntry
}

type Neighbour struct {
	IP  		netip.Addr 			// virtual IP (10.2.0.3)
	UDPAddr		net.UDPAddr   		// real UDP addr (127.0.0.1:5006)
}

type Interface struct {
	Name  		string								// "if0"
	Prefix		netip.Prefix 						// 10.2.0.1/24
	Conn  		net.Conn  							// opened UDP socket
	Neighbours 	map[netip.Addr]Neighbour
}

const (
	SourceTypeDirect = 0
	SourceTypeRIP = 1
)

type FwdEntry struct {
	Prefix			netip.Addr		// 10.2.0.1/24
	NextHop   		netip.Addr		// zero if directly connected
	InterfaceName 	string   		// "if0", "if1"
	Type	   		int	
}

