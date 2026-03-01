package ipStack

import (
	ll "ip-isabelle-and-ben/pkg/linkLayer"
	"net/netip"
	"sync"
	"time"
)

/*
	Contains:
	- map "ifo" : Interface{"ifo", 10.0.0.0/24, net.Conn(Link Layer), neighbors on that local net }
	- List of fwding table entries to iterate through
*/
type IPStack struct {
	// potentially consider adding a field "name" e.g. "H1" or "R1" for debugging purposes even if not strictly correct
	Interfaces			map[string]*ll.Interface  	/* “if0” : Interface() */
	ForwardingTable 	map[netip.Prefix]FwdEntry

	IncomingPacketChan 	chan ll.IPPacket			/* for listener goroutine to tell main thread that we got a package */
	RipInfo 			RipInfo						/* for routers to store RipInfo */
	mu 					sync.Mutex 					/* protect forwarding table */
}

/*
	For entries in Forwarding Table
	- Direct = directly connected via local interface
	- RIP = learned about through RIP--routed through multiple hops
	- Static = used to specify default route for hosts
*/
const (
	SourceTypeLocal = 0
	SourceTypeRIP = 1
	SourceTypeStatic = 2

	ProtocolTypeTest = 0
	ProtocolTypeRIP = 200

	TTLNew = 32
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
	LastUpdated 	time.Time 		/* for RIP */
}

type RipNeighbour struct {
	RouterIP netip.Addr
	InterfaceName string /* for ease */
}

type RipInfo struct {
	/* would be far more helpful if we had an interface here than neighboring IP -> will have to get that later */
	Neighbors 		[]RipNeighbour
	RipTimeout		time.Duration
	RipUpdateRate	time.Duration
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

