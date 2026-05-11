# IP
## Project Structure

```
.
├── cmd/
│   ├── vhost/main.go           # command entrypoint to init/run host IP stack
│   └── vrouter/main.go         # command entrypoint to init/run router IP stack
└── pkg/
    ├── ipStack/                # all IP forwarding and RIP logic/
    │   ├── ipStack.go          # init IP structs, forwarding, LPM, marshalling
    │   ├── replAndPrint.go     # all REPL commands and printing logic  
    │   ├── rip.go              # init RIP structs, handle updates + timeouts
    │   └── types.go            # types for the IP stack, see next section
    ├── linkLayer/
    │   ├── linkLayer.go        # light wrappers handling read/writes to UDP
    │   └── types.go            # interface and neighbour types
    ├── protocol/utils.go       # small misc util functions
    └── lnxconfig/lnxconfig.go  # lnx file parsing copied from sample code
```
## Data Structures:

### IP Stack
Our IP Stack (see [`pkg/ipStack/types.go`](https://github.com/brown-cs1680-s26/ip-isabelle-and-ben/blob/main/pkg/ipStack/types.go)) contains the following data structures:

```
type IPStack struct {
	Interfaces		map[string]*ll.Interface
	ForwardingTable 	map[netip.Prefix]*FwdEntry
	mu 			sync.Mutex
	IncomingPacketChan 	chan []byte
	recvHandlers 		map[int]ReceiveHandler
	RipInfo 		RipInfo
}
```

* **`Interfaces`**: maps interface name (“if0”) to Interface object  
* **`ForwardingTable`**: maps network prefixes to FwdTable entries  
  * Used by hosts and routers for IP forwarding  
  * Used by routers for RIP  
    * Contains fields like Cost and LastUpdated used only by routers  
* **Mutex `mu`**: used by routers to protect their forwarding table during concurrent updates/reads
* **`IncomingPacketChan`**: tells main/processing goroutine that the listener process has received a packet at its port  
* **`recvHandlers`**: maps protocol number (0, 200\) to the function used to “handle” messages that arrive via that protocol  
* **`RipInfo`**: used only by routers to store config-provided RIP info (neighbors, timeout, and update rate)

### RIP

Where `RipInfo` is accompanied by additional RIP types, largely based off the types defined in the `lnxconfig` file:

```
// Highest-level RIP type; each IP Stack has one.
type RipInfo struct {
	Neighbors 	[]RipNeighbour
	RipTimeout	time.Duration
	RipUpdateRate	time.Duration
}

// A struct storing which neighbours a given router has.
// --> This is our way to know TO WHOM to send updates to.
type RipNeighbour struct {
	RouterIP netip.Addr
	InterfaceName string
}

// A message to be received by a router for RIP.
// --> Entries field used by receiving IPstack to update its FwdTable.
type RipMessage struct {
	Command uint16
	NumEntries uint16
	Entries []RipEntry
}

// Info used to update an IP stack's forwarding table from a RIP message/
type RipEntry struct {
	Cost 		uint32
	Prefix 		netip.Prefix
}
```

### Link Layer
We factored the following UDP-related data structures into a separate Link Layer package  (see [`pkg/linkLayer/types.go`](https://github.com/brown-cs1680-s26/ip-isabelle-and-ben/blob/main/pkg/linkLayer/types.go)):
```
type Interface struct {
	Name  		string			  // e.g. "if0"
	Prefix		netip.Prefix 	  	  // e.g. 10.2.0.1/24
	IP 		netip.Addr
	Conn  		*net.UDPConn  		  // opened UDP socket
	Neighbours 	map[netip.Addr]*Neighbour // neighbour IP : Neighbour()
	Up 		bool			  // is interface enabled
}

type Neighbour struct {
	IP  		netip.Addr 		  // virtual IP (10.2.0.3)
	UDPAddr		netip.AddrPort
}
```
* **`Interface`:** contains own prefix and VIP, also its Conn object used for sending and receiving packets.  
  * Contains **`Neighbours` map**: maps VIP to neighbour object for directly accessible neighbors on the same local network  
* **`Neighbour`:** contains VIP and a UDP address \+ port number for “link layer” communication

## `vhost` and `vrouter`

Both the `vhost` and `vrouter` use our `InitIPStackFromConfig()` method to create their IP stacks. Most of the rest of the code is in our IPStack, however the host and router call slightly different methods on their IPStacks to get themselves up and running.

* **Vhost:**  
  * Registers a receive handler for protocol 0 to print out test packets when they reach host  
  * Runs IP layer: when packets come in, hosts deserialize and validate them to see if they reached the right destination  
  * Runs its REPL  
* **Vrouter:**  
  * Registers a receive handler for protocol 200 to properly handle RIP messages when they are received  
  * Runs IP layer: when packets come in, routers deserialize and validate them before determining if they need to be forwarded and where  
  * Runs RIP loops:  
    * `UpdateLoop()`: periodically updates neighbors of its own routing table  
    * `TimeoutLoop()`: periodically checks routing table to see if entries have timed out  
  * Runs its REPL

## Goroutines/Threading

Concurrency, be it goroutines, channels, or mutexes, show up in our codebase in the following places:
* At the `vhost/vrouter` level, when we start their respective REPLs with a goroutine;
* At the link layer level, where each node has a goroutine to listen for incoming packets;
	* When we receive a packet, the link layer forwards it to the IP layer via each stack's `ipStack.IncomingPacketChan` channel to be handled;
* At the IP stack level, where:
	* each stack has a goroutine to listen on `ipStack.IncomingPacketChan` to handle bytes bubbled up from the link layer;
	* each router has several goroutines for RIP:
		* one to send periodic updates to RIP neighbours,
		* one to constantly check the stack's `ForwardingTable` for timeouts,
		* one to send triggered updates for when an entry changes or times out,
			* where in all cases, if the `ForwardingTable` is being touched, we first acquire the IP stack's `ipStack.mu` mutex to ensure consistency.  

## IP Packet Processing

When a packet arrives at a node, it goes through the following steps, as orchestrated by [ipStack.go](https://github.com/brown-cs1680-s26/ip-isabelle-and-ben/blob/main/pkg/ipStack/ipStack.go)’s `RunIPLayer()` method:

1. Packet received in ipStack’s packet channel
2. Packet is deserialized and checksum is validated, TTL validated  
3. Destination check: if destination reached, consult Handler function map and call proper handler linked to protocol type  
4. Forwarding: TTL decremented, longest prefix match performed  
   1. Either a local match, default, or next hop match is found, or packet is dropped  
   2. If the desired route’s destination is offline (cost \= Infinity), the packet is dropped  
5. Link Layer Sending:   
   1. Node finds the correct interface in their interfaces map and looks up dest in that interface’s neighbors table, from which we get its UDP address and send the packet

## RIP

We decided to include RIP functionality in the IPStack package even though it ois only relevant for routers because it is inherently tied to internal IPStack data structures, and we didn’t want to expose those to another package’s methods.

**Routing Table (Stored in IP Stack `ForwardingTable`)**

* In addition to the information stored in the hosts’ forwarding tables, we store each route’s cost, last time updated, and type (RIP, local or static)
	* Note that we do not have an additional `RoutingTable` type, but rather just added these additional fields to each IP stack's forwarding table, where applicable
* Our routing table is also protected with a mutex because we may have two goroutines using or updating the same routing table at the same time, as mentioned in the Goroutines/Threading section above.
