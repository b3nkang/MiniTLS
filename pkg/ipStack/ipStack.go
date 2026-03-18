package ipStack

import (
	"errors"
	"fmt"
	ll "ip-isabelle-and-ben/pkg/linkLayer"
	"ip-isabelle-and-ben/pkg/lnxconfig"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"log"
	"net"
	"net/netip"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
)

/* Not a method *ON* IPstack, but shared between host and router */
func InitIPStackFromConfig(fileName string)(*IPStack, error) {
	/* parse lnx file */
	lnxConfig, err := lnxconfig.ParseConfig(fileName)
	if err != nil {
		return nil, err
	}
	
	ipStack := &IPStack{
		Interfaces:      make(map[string]*ll.Interface),
		ForwardingTable: make(map[netip.Prefix]*FwdEntry, 0),
		IncomingPacketChan: make(chan []byte, 100),
		recvHandlers: make(map[int]ReceiveHandler),
		TCPReplChan: make(chan string, 5),
	}

	/* initialize structs within this IPStack */
	err = ipStack.init(lnxConfig)
	if err != nil {
		return nil, err
	}

	/* if we are a router, also init RIP */
	if lnxConfig.RoutingMode == lnxconfig.RoutingTypeRIP {
		ipStack.initRIP(lnxConfig)
	}

	return ipStack, nil;
}

/* take in parsed lnx config file and populate IPStack */
func (stack *IPStack) init(config *lnxconfig.IPConfig) error {

	/* Initialize Interfaces */
	for _, ifx := range config.Interfaces {
		/* init udp conn for link layer */
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{
			IP: net.IP(ifx.UDPAddr.Addr().AsSlice()), 	/* convert netip.Addr -> net.IP */
			Port: int(ifx.UDPAddr.Port()),
		})
		if err != nil {
			return err
		}
		/* create and fill in interface struct--neighbors empty for now */
		iface := &ll.Interface{
			Name: ifx.Name,
			Prefix: ifx.AssignedPrefix,
			IP: ifx.AssignedIP,
			Conn: conn,
			Neighbours: make(map[netip.Addr]*ll.Neighbour),
			Up: true,
		}
		
		stack.Interfaces[ifx.Name] = iface
	}

	/* Initialize Neighbors */
	for _, n := range config.Neighbors {
		iface, ok := stack.Interfaces[n.InterfaceName]
		if !ok {
			log.Printf("Neighbor %s contains unknown interface: %s", n.DestAddr, n.InterfaceName)
		}
		neighbor := ll.Neighbour{
			IP: n.DestAddr, 	/* netip.Addr */
			UDPAddr: n.UDPAddr, /* net.UDPAddr */
		}
		/* map neighbor's IP address to neighbor struct */
		iface.Neighbours[n.DestAddr] = &neighbor
	}

	stack.initFwdTable(config)

	/* start listeners for each interface */
	for _, iface := range stack.Interfaces {
		go iface.LinkLayerListen(stack.IncomingPacketChan)
	}

	return nil
}

/* Initialize forwarding table -- router specific stuff elsewhere */
func (stack *IPStack) initFwdTable(config *lnxconfig.IPConfig) {
	for _, iface := range stack.Interfaces {
		entry := FwdEntry{
			Prefix: iface.Prefix,
			NextHop: netip.Addr{}, /* empty = LOCAL */
			InterfaceName: iface.Name,
			Type: SourceTypeLocal,
			Cost: 0,
		}
		stack.ForwardingTable[iface.Prefix] = &entry
	}
	/* add default/static routes */
	for prefix, address := range config.StaticRoutes {
		entry := FwdEntry{
			Prefix: prefix,
			NextHop: address,
			/* no interface name */
			Type: SourceTypeStatic,
			/* no cost */
		}
		stack.ForwardingTable[entry.Prefix] = &entry
	}
}

/* register handler function for protocol */
func (stack *IPStack) RegisterRecvHandler(protoNum int, handler ReceiveHandler) {
    stack.recvHandlers[protoNum] = handler
}

/* Highest-level send function on IP Stack, called by REPL. Internal sends use iface.LinkLayerSend(). */
func (ipStack *IPStack) SendIP(finalDest netip.Addr, message []byte, protoNum int) error {
	// longest prefix match
	entry, nextDest, found := ipStack.LongestPrefixMatch(finalDest)
	if !found {
		fmt.Printf("[IP] No match on LongestPrefixMatch in FwdTable, dropping packet\n")
		return errors.New("No match on LongestPrefixMatch in FwdTable")
	}
	// fmt.Printf("[IP] Longest prefix match found on %s\n", entry.InterfaceName)

	// check how to forward
	switch entry.Type {
	case SourceTypeLocal:
		// if direct, seek through neighbours and send 
	//	fmt.Printf("[IP] Match is directly connected. Forwarding to destination %s\n", nextDest.String())
		iface := ipStack.Interfaces[entry.InterfaceName]
		nextDestAsNeighbour := iface.Neighbours[nextDest]
		if nextDestAsNeighbour == nil {
			fmt.Printf("[IP] We hit default forwarding case, but the next hop address <%s> does not exist. Dropping packet...\n", nextDest.String())
			// no other error for now
		} else {
			nextDestAsUDPAddr := net.UDPAddrFromAddrPort(nextDestAsNeighbour.UDPAddr)
			// brand new send, so specify new TTL
			bytesToSend := SerializePacket(iface.IP, finalDest, []byte(message), protoNum, 0, true)
			iface.LinkLayerSend(nextDestAsUDPAddr, bytesToSend)
		}
	default:
		fmt.Printf("[IP] Found a match but could not send, bad entry type. Error should not happen. Dropping packet\n")
	}
	return nil
}

/* Run the IP Layer (handle and process messages) */
func (ipStack *IPStack) RunIPLayer() {
	// fmt.Println("Running IP Layer...")
	for msgBytes := range ipStack.IncomingPacketChan {
		message, hdr, err := DeserializeAndValidatePacket(msgBytes)
		if err != nil {
			continue	// drop packet if parsing or validation fails
		}

		ipStack.ipForwarding(hdr, message)
	}
}

// ********************** DELIVERY OR FORWARDING LOGIC **********************
/* IP Forwarding method: 
*/
func (ipStack *IPStack) ipForwarding(hdr *ipv4header.IPv4Header, message []byte) {	
	// fmt.Printf("[IP] Received IP packet...\nHeader:  %v\nChecksum:  OK\nMessage:  %s\n", hdr, string(message))

	// ---- DESTINATION REACHED CASE ----
	destFound := false
	finalDest := hdr.Dst
	for _, iface := range ipStack.Interfaces {
		if iface.IP == finalDest {
			// fmt.Printf("[IP] packet destination reached on interface %s\n", iface.Name)
			destFound = true
			/* call correct receive handler */
			receiveHandler, exists := ipStack.recvHandlers[hdr.Protocol]
			if !exists {
				fmt.Printf("[IP] No handler registered for protocol %d\n", hdr.Protocol)
				break
			}
			receiveHandler(hdr, message)
			break
		}
	}
	if destFound {
		// fmt.Printf("> ") // print for REPL
		return // we're done, wait for next paket
	}

	// ---- FORWARDING CASE ----
	// decrement TTL
	hdr.TTL -= 1
	if hdr.TTL <= 0 {
		fmt.Printf("[IP] pre-decrement TTL expired, dropping packet\n> ")
		return
	}
	// longest prefix match
	entry, nextDest, found := ipStack.LongestPrefixMatch(finalDest)
	if !found {
		fmt.Printf("[IP] No match on LongestPrefixMatch in FwdTable, dropping packet\n> ")
		return
	}

	// if cost is infinity, the route is offline, so drop
	if entry.Cost >= Infinity {
		fmt.Printf("[IP] Entry in FwdTable found, but cost was %d, >= INF. Dropping packet...\n> ", entry.Cost)
		return
	}

	// check how to forward
	switch entry.Type {
		case SourceTypeLocal:
			// if direct, seek through neighbours and send 
			// fmt.Printf("[IP] Match prefix is directly connected. Forwarding to prefix %s for dest %s\n", entry.Prefix.String(), hdr.Dst.String())
			iface := ipStack.Interfaces[entry.InterfaceName]

			/* if interface is down, drop packet */
			if !iface.Up {
				fmt.Printf("Required to forward packet on down interface: %s. Dropping packet.\n", iface.Name)
				return
			}

			nextDestAsNeighbour := iface.Neighbours[nextDest]
			if nextDestAsNeighbour == nil {
				// fmt.Printf("[IP] Entry in FwdTable found, LOCAL case, but neighbor <%s> doesn't exist. Dropping packet...\n> ", nextDest.String())
				return
			}
			/* get neighbor's UDP address */
			nextDestAsUDPAddr := net.UDPAddrFromAddrPort(nextDestAsNeighbour.UDPAddr)
			bytesToSend := SerializePacket(hdr.Src, finalDest, message, ProtocolTypeTest, hdr.TTL, false)
			iface.LinkLayerSend(nextDestAsUDPAddr, bytesToSend)
		/* we are a router and we learned about this route through RIP */
		case SourceTypeRIP:
			fmt.Println("[IP] BAD THING HAPPENED THIS SHOULDN'T HAPPEN hit SourceTypeRIP case in IPForwarding")
		default:
			fmt.Printf("[IP] Found a match but could not send, bad entry type. Error should not happen. Dropping packet...\n> ")
	}
	fmt.Printf("> ")
}

// returns the FwdEntry and dest netIPAddr with the longest prefix match and true. if none, bool return value is false
func (ipStack *IPStack) LongestPrefixMatch(dest netip.Addr) (*FwdEntry, netip.Addr, bool) {
	maxLen := -1
	var longestMatchEntry *FwdEntry
	for prefix, entry := range ipStack.ForwardingTable {
		if prefix.Contains(dest) && prefix.Bits() > maxLen {
			maxLen = prefix.Bits()
			longestMatchEntry = entry
		}
	}
	if maxLen == -1 {
        return nil, netip.Addr{}, false
    }
    // if route directly connected, ret
    if longestMatchEntry.InterfaceName != "" {
        return longestMatchEntry, dest, true
    }
    // else recurse on nextHop
	if longestMatchEntry.Cost >= Infinity {
		fmt.Printf("[IP] LPM found match at %s, but the entry is offline. Dropping packet...\n", longestMatchEntry.Prefix.Addr().String())
        return nil, netip.Addr{}, false
	}
    return ipStack.LongestPrefixMatch(longestMatchEntry.NextHop)
}

/* serializes an IP packet (IPV4 Header + bytes message) for the UDP layer to send */
/* need to change string message to bytes to acccommodate RIP packets */
func SerializePacket(source netip.Addr, dest netip.Addr, message []byte, protocol int, ttl int, isTtlNew bool) ([]byte) {
	/* Start filling in the header, use passed in fields */
	hdr := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(message),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      TTLNew,
		Protocol: protocol,
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      source,
		Dst:      dest,
		Options:  []byte{},
	}
	if !isTtlNew {
		hdr.TTL = ttl
	}
	// Assemble the header into a byte array
	headerBytes, err := hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
	}
	// Compute the checksum (see below)
	// Cast back to an int, which is what the Header structure expects
	hdr.Checksum = int(utils.ComputeChecksum(headerBytes))
	headerBytes, err = hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
	}
	/* Append header + message into one byte array */
	bytesToSend := make([]byte, 0, len(headerBytes)+len(message))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, message...)

	return bytesToSend
}

/* Takes in raw bytes, validates the checksum, checks TTL, and returns the deserialized message and header */
func DeserializeAndValidatePacket(msgBytes []byte) ([]byte, *ipv4header.IPv4Header, error) {
	/* Marshal the received byte array into a UDP header
	NOTE:  This does not validate the checksum or check any fields */
	hdr, err := ipv4header.ParseHeader(msgBytes)

	if err != nil {
		/* drop packet if parsing doesn't work */
		fmt.Println("Error parsing header", err)
		return make([]byte,0), nil, err
	}

	/* extract and validate checksum */
	headerSize := hdr.Len
	headerBytes := msgBytes[:headerSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := utils.ValidateChecksum(headerBytes, checksumFromHeader)

	/* determine if we passed or failed checksum */
	if computedChecksum == checksumFromHeader {
		// fmt.Println("Checksum passed")
	} else {
		fmt.Printf("Checksum failed, dropping packet...\n")
		return make([]byte,0), nil, err
	}

	// check TTL
	if hdr.TTL <= 0 {
		fmt.Printf("TTL expired, dropping packet...\n")
		return make([]byte,0), nil, err
	}

	/* Next, get the message, which starts after the header */
	message := msgBytes[headerSize:]

	/* we probably don't want to return a string here--just want to return bytes and let 
		handler convert */
	return message, hdr, nil
}

// updates or creates a new FwdEntry given an incoming RipEntry
func (ipStack *IPStack) UpdateForwardingTable(ripEntry RipEntry, hdr *ipv4header.IPv4Header, ifaceName string, ripEntryCost uint32) {
	ipStack.ForwardingTable[ripEntry.Prefix] = &FwdEntry{
		Prefix: ripEntry.Prefix,
		NextHop: hdr.Src, 				// whom we're receiving this ripMsg from
		InterfaceName: ifaceName,
		Type: SourceTypeRIP,
		Cost: ripEntryCost,
		LastUpdated: time.Now(),
	}
}


/******* INTERFACE UP/DOWN FUNCTIONALITY ********/


func (stack *IPStack) IFDown(ifaceName string) error {
	iface, exists := stack.Interfaces[ifaceName]
	if !exists {
		return errors.New("invalid interface name")
	}
	iface.Up = false
	return  nil
}


func (stack *IPStack) IFUp(ifaceName string) error {
	iface, exists := stack.Interfaces[ifaceName]
	if !exists {
		return errors.New("invalid interface name")
	}
	iface.Up = true
	return  nil
}