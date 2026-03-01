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
		IncomingPacketChan: make(chan ll.IPPacket, 100),
	}

	/* initialize structs within this IPStack */
	err = ipStack.Init(lnxConfig)
	if err != nil {
		return nil, err
	}

	return ipStack, nil;
}

/* take in parsed lnx config file and populate IPStack */
func (stack *IPStack) Init(config *lnxconfig.IPConfig) error {

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

	err := stack.InitFwdTable(config)
	if err != nil {
		return err
	}

	/* start listeners for each interface */
	for _, iface := range stack.Interfaces {
		go iface.LinkLayerListen(stack.IncomingPacketChan)
	}

	// if you are router, start RIP
	if config.RoutingMode == lnxconfig.RoutingTypeRIP {
		go stack.UpdateLoop()
	}

	return nil
}

/* Initialize forwarding table based on RoutingMode */
func (stack *IPStack) InitFwdTable(config *lnxconfig.IPConfig) error {
	switch config.RoutingMode {
		/* case: host */
		case lnxconfig.RoutingTypeStatic:
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
			/* add default/static routes on host */
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
		/* case: router */
		case lnxconfig.RoutingTypeRIP:
			for _, iface := range stack.Interfaces {
				/* we only know about directly connected interfaces currently */
				entry := FwdEntry{
					Prefix: iface.Prefix,
					NextHop: netip.Addr{}, /* empty = LOCAL */
					InterfaceName: iface.Name,
					Type: SourceTypeLocal,
					Cost: 0, /* all local interfaces have cost = 0*/
				}
				stack.ForwardingTable[iface.Prefix] = &entry
			}
			/* add static routes for router */
			for prefix, address := range config.StaticRoutes {
				entry := FwdEntry{
					Prefix: prefix,
					NextHop: address,
					/* no interface name */
					Type: SourceTypeStatic,
					Cost: 0, /* local */
				}
				stack.ForwardingTable[entry.Prefix] = &entry
			}
			/* since we know we're a router, Init RIPInfo here */
			err := stack.InitRIP(config)
			if err != nil {
				return err
			}

		default:
			log.Printf("Routing Mode that's not static or RIP: %d\n", config.RoutingMode)
			return errors.New("Incorrect Routing Mode")
		}
	return nil
}

/* Highest-level send function on IP Stack, called by REPL. Internal sends use iface.LinkLayerSend(). */
// TODO: low priority, but lots of the forwarding logic here is duplicated with RunIPLayer(). There are some nontrivial 
// differences though, hence why the logic hasn't been abstracted into 1 func yet, but consider in future if messoooy
func (ipStack *IPStack) SendIP(finalDest netip.Addr, message string) error {
	// longest prefix match
	entry, nextDest, found := ipStack.LongestPrefixMatch(finalDest)
	if !found {
		fmt.Printf("[IP] No match on LongestPrefixMatch in FwdTable, dropping packet\n")
		return errors.New("No match on LongestPrefixMatch in FwdTable")
	}
	fmt.Printf("[IP] Longest prefix match found on %s\n", entry.InterfaceName)

	// check how to forward
	switch entry.Type {
	case SourceTypeLocal:
		// if direct, seek through neighbours and send 
		fmt.Printf("[IP] Match is directly connected. Forwarding to destination %s\n", nextDest.String())
		iface := ipStack.Interfaces[entry.InterfaceName]
		nextDestAsNeighbour := iface.Neighbours[nextDest]
		if nextDestAsNeighbour == nil {
			fmt.Printf("[IP] We hit default forwarding case, but the next hop address <%s> does not exist. Dropping packet...\n", nextDest.String())
			// no other error for now
		} else {
			nextDestAsUDPAddr := net.UDPAddrFromAddrPort(nextDestAsNeighbour.UDPAddr)
			// brand new send, so specify new TTL
			bytesToSend := SerializePacket(iface.IP, finalDest, []byte(message), ProtocolTypeTest, 0, true)
			iface.LinkLayerSend(nextDestAsUDPAddr, bytesToSend)
		}
	case SourceTypeRIP:
		// if via RIP, forward to next hop, complete once next hop-updating logic is implemented
		// TODO: verify after RIP handling complete that this is still necessary; LPM recursion may handle this already
	default:
		fmt.Printf("[IP] Found a match but could not send, bad entry type. Error should not happen. Dropping packet\n")
	}
	return nil
}

/* Run the IP Layer (handle and process messages) */
func (ipStack *IPStack) RunIPLayer() {
	// fmt.Println("Running IP Layer...")
	for packet := range ipStack.IncomingPacketChan {
		message, hdr, err := DeserializeAndValidatePacket(packet)
		if err != nil {
			continue	// drop packet if parsing or validation fails
		}
		switch hdr.Protocol {
		case ProtocolTypeTest:
			ipStack.HandleTestMessage(hdr, string(message), packet)
		case ProtocolTypeRIP:
			ipStack.HandleRipMessage(hdr, message)
		default:
			fmt.Printf("[IP] Received packet with unrecognized protocol type. Dropping packet...\n> ")
			continue
		}
	}
}

// ******************** HANDLE INCOMING RIP MESSAGE LOGIC *******************
func (ipStack *IPStack) HandleRipMessage(hdr *ipv4header.IPv4Header, messageBytes []byte) {
	// fmt.Printf("[IP] Received RIP message, handling...\n> ")

	ripMsg, err := DeserializeRipMessage(messageBytes)
	if err != nil {
		fmt.Printf("[IP] Received malformed RIP data, unable to deserialize. Error: %s\n> ", err)
		return
	}

	// lock while we update table
	ipStack.mu.Lock()
	defer ipStack.mu.Unlock()
	ipStack.PrintForwardingTable()
	for _, ripEntry := range ripMsg.Entries {
		currFwdEntry, exists := ipStack.ForwardingTable[ripEntry.Prefix]
		// check if exists, if not, add
		if !exists {
			fmt.Printf("[RIP] Received RipEntry AND added to table.\nNew table: \n")
			ipStack.UpdateForwardingTable(ripEntry, hdr, "")
			continue
		} 
		// if it already is in table, check if we can even update it
		if currFwdEntry.Type != SourceTypeRIP {
			// fmt.Printf("[RIP] Received RipEntry but FwdEntry was not of type RIP\n> ")
			continue
		}
		// entry exists:
		// if lower cost, just update
		ripEntryCost := ripEntry.Cost+1
		currFwdEntryCost := currFwdEntry.Cost
		if ripEntryCost < currFwdEntryCost {
			ipStack.UpdateForwardingTable(ripEntry, hdr, "")
			continue
		}
		// else, if it doesn't equal
		// TODO: NOTE this is WRONG for SH/PR, UPDATE later but naive implementation for now
		if ripEntryCost != currFwdEntryCost {
			// if the next hops are diff, topology changed, updated
			if hdr.Src != currFwdEntry.NextHop {
				ipStack.UpdateForwardingTable(ripEntry, hdr, "")
				continue
			} 
			// else, curr cost is better, ignore
		}
		// else if everything is the same
		if ripEntryCost == currFwdEntryCost && hdr.Src == currFwdEntry.NextHop {
			currFwdEntry.LastUpdated = time.Now() 	// TODO: double check this is what we want
		}
	}
}

// ********************** DELIVERY OR FORWARDING LOGIC **********************
// TODO: packet only being passed currently for light logging. remove once no longer needed
func (ipStack *IPStack) HandleTestMessage(hdr *ipv4header.IPv4Header, message string, packet ll.IPPacket) {	
	/* print out all the stuff */
	fmt.Printf("[IP] Received IP packet from %s\nHeader:  %v\nChecksum:  OK\nMessage:  %s\n",
		packet.SrcIfaceAddr.String(), hdr, string(message))

	// ---- DESTINATION REACHED CASE ----
	destFound := false
	finalDest := hdr.Dst
	for _, iface := range ipStack.Interfaces {
		if iface.IP == finalDest {
			fmt.Printf("[IP] packet destination reached on interface %s\n", iface.Name)
			destFound = true
			// TODO: call some handler
			break
		}
	}
	if destFound {
		fmt.Printf("> ") // print for REPL
		return // we're done, wait for next paket
	}

	// ---- FORWARDING CASE ----
	// decrement TTL
	hdr.TTL -= 1
	if hdr.TTL <= 0 {
		fmt.Printf("[IP] pre-decrement TTL expired, dropping packet\n> ")
		// TODO: no current way to ident IPStack addr, consider IPStack type update
		return
	}
	// longest prefix match
	entry, nextDest, found := ipStack.LongestPrefixMatch(finalDest)
	if !found {
		fmt.Printf("[IP] No match on LongestPrefixMatch in FwdTable, dropping packet\n> ")
		return
	}
	fmt.Printf("[IP] Longest prefix match found on %s\n", entry.InterfaceName)
	// check how to forward
	switch entry.Type {
	case SourceTypeLocal:
		// if direct, seek through neighbours and send 
		fmt.Printf("[IP] Match prefix is directly connected. Forwarding to prefix %s for dest %s\n", entry.Prefix.String(), hdr.Dst.String())
		iface := ipStack.Interfaces[entry.InterfaceName]
		nextDestAsNeighbour := iface.Neighbours[nextDest]
		if nextDestAsNeighbour == nil {
			fmt.Printf("[IP] We hit default forwarding case, but the next hop address <%s> does not exist. Dropping packet...\n> ", nextDest.String())
			return
		}
		nextDestAsUDPAddr := net.UDPAddrFromAddrPort(nextDestAsNeighbour.UDPAddr)
		bytesToSend := SerializePacket(iface.IP, finalDest, []byte(message), ProtocolTypeTest, hdr.TTL, false)
		iface.LinkLayerSend(nextDestAsUDPAddr, bytesToSend)
	case SourceTypeRIP:
		// if via RIP, forward to next hop, complete once next hop-updating logic is implemented
		// TODO: verify after RIP handling complete that this is still necessary; LPM recursion may handle this already
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
    return ipStack.LongestPrefixMatch(longestMatchEntry.NextHop)
}

// TODO: potentially move these serializers/deserializers to Protocol? idk

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
func DeserializeAndValidatePacket(packet ll.IPPacket) ([]byte, *ipv4header.IPv4Header, error) {
	/* Marshal the received byte array into a UDP header
	NOTE:  This does not validate the checksum or check any fields */
	hdr, err := ipv4header.ParseHeader(packet.Data)

	if err != nil {
		/* drop packet if parsing doesn't work */
		fmt.Println("Error parsing header", err)
		return make([]byte,0), nil, err
	}

	/* extract and validate checksum */
	headerSize := hdr.Len
	headerBytes := packet.Data[:headerSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := utils.ValidateChecksum(headerBytes, checksumFromHeader)

	/* determine if we passed or failed checksum */
	if computedChecksum == checksumFromHeader {
		// fmt.Println("Checksum passed")
	} else {
		fmt.Printf("Checksum failed, dropping packet from %s\n", packet.SrcIfaceAddr.String())
		return make([]byte,0), nil, err
	}

	// check TTL
	if hdr.TTL <= 0 {
		fmt.Printf("TTL expired, dropping packet from %s\n", packet.SrcIfaceAddr.String())
		return make([]byte,0), nil, err
	}

	/* Next, get the message, which starts after the header */
	message := packet.Data[headerSize:]

	/* we probably don't want to return a string here--just want to return bytes and let 
		handler convert */
	return message, hdr, nil
}

// updates or creates a new FwdEntry given an incoming RipEntry
func (ipStack *IPStack) UpdateForwardingTable(ripEntry RipEntry, hdr *ipv4header.IPv4Header, ifaceName string) {
	ipStack.ForwardingTable[ripEntry.Prefix] = &FwdEntry{
		Prefix: ripEntry.Prefix,
		NextHop: hdr.Src, 				// whom we're receiving this ripMsg from
		InterfaceName: ifaceName,
		Type: SourceTypeRIP,
		Cost: ripEntry.Cost + 1,
		LastUpdated: time.Now(),		// TODO: double check this is what we want
	}
}