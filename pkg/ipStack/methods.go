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
		ForwardingTable: make(map[netip.Prefix]FwdEntry, 0),
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
			Neighbours: make(map[netip.Addr]ll.Neighbour),
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
		iface.Neighbours[n.DestAddr] = neighbor
	}

	err := stack.InitFwdTable(config)
	if err != nil {
		return err
	}

	/* start listeners for each interface */
	for _, iface := range stack.Interfaces {
		go iface.LinkLayerListen(stack.IncomingPacketChan)
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
				stack.ForwardingTable[iface.Prefix] = entry
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
				stack.ForwardingTable[entry.Prefix] = entry
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
				stack.ForwardingTable[iface.Prefix] = entry
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
				stack.ForwardingTable[entry.Prefix] = entry
			}
			/* TODO: call a function to setup RIP stuff maybe */

		default:
			log.Printf("Routing Mode that's not static or RIP: %d\n", config.RoutingMode)
			return errors.New("Incorrect Routing Mode")
		}
	return nil
}



/* Highest-level send function on IP Stack, called by REPL. Internal sends use iface.LinkLayerSend(). */
/* Send a message on the IP Layer HARDCODED H1 -> R1 TODO: update to not be hardcoded */
/*
	Test: send message from h1 to r1 in doc-example
	h1: 
		- UDP: 127.0.0.1:5000
	r1: 
		- IP to send to: 10.0.0.2 
		- UDP to send to: 127.0.0.1:5001
*/
func (ipStack *IPStack) SendIP(dest netip.Addr, message string) error {
	/* hardcode fields that will come from forwarding and neighbor table for now */

	/* conn for this stack's if0 (assume h1) */
	myIf := ipStack.Interfaces["if0"]

	/* string for r1's IP and port */
	destStringAddr := "127.0.0.1:5001"

	destUDPAddr, err := net.ResolveUDPAddr("udp", destStringAddr)
	if err != nil {
		panic(err)
	}
	
	bytesToSend := SerializePacket(myIf.IP, dest, message, 0, 64, true)
	myIf.LinkLayerSend(destUDPAddr, bytesToSend)

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
		/* print out all the stuff */
		fmt.Printf("[IP] Received IP packet from %s\nHeader:  %v\nChecksum:  OK\nMessage:  %s\n",
			packet.SrcIfaceAddr.String(), hdr, string(message))

		// ********************** DELIVERY OR FORWARDING LOGIC **********************
		// consider moving out of this func into a separate function for clarity later
		
		// ---- DESTINATION REACHED CASE ----
		destFound := false
		for _, iface := range ipStack.Interfaces {
			if iface.IP == hdr.Dst {
				fmt.Printf("[IP] packet destination reached on interface %s\n", iface.Name)
				destFound = true
				// TODO: call handler
				break
			}
		}
		if destFound {
			continue // we're done, wait for next paket
		}

		// ---- FORWARDING CASE ----
		// decrement TTL
		hdr.TTL -= 1
		if hdr.TTL <= 0 {
			fmt.Printf("[IP] pre-decrement TTL expired, dropping packet")
			// TODO: no current way to ident IPStack addr, consider IPStack type update
			continue
		}
		// longest prefix match
		entry, found := ipStack.LongestPrefixMatch(hdr.Dst)
		if !found {
			fmt.Printf("[IP] No match on LongestPrefixMatch in FwdTable, dropping packet\n")
			continue
		}
		fmt.Printf("[IP] Longest prefix match found on %s\n", entry.InterfaceName)
		// check how to forward
		switch entry.Type {
		case SourceTypeLocal:
			// if direct, seek through neighbours and send 
			fmt.Printf("[IP] Match is directly connected. Forwarding to destination %s\n", hdr.Dst.String())
			iface := ipStack.Interfaces[entry.InterfaceName]
			destNeighbour := iface.Neighbours[hdr.Dst]
			destUDPAddr := net.UDPAddrFromAddrPort(destNeighbour.UDPAddr)
			bytesToSend := SerializePacket(iface.IP, hdr.Dst, string(message), 0, hdr.TTL, false) // TODO: check what protocol is bc idk
			iface.LinkLayerSend(destUDPAddr, bytesToSend)
		case SourceTypeRIP:
			// TODO: if via RIP, forward to next hop
			// complete once next hop-updating logic is implemented
		default:
			fmt.Printf("[IP] Found a match but could not send, bad entry type. Error should not happen. Dropping packet\n")
		}
	}
}

// returns the FwdEntry with the longest prefix match and true. if none, bool return value is false
func (ipStack *IPStack) LongestPrefixMatch(dest netip.Addr) (FwdEntry, bool) {
	maxLen := 0
	var longestMatchEntry FwdEntry
	for prefix, entry := range ipStack.ForwardingTable {
		if prefix.Contains(dest) && prefix.Bits() > maxLen {
			maxLen = prefix.Bits()
			longestMatchEntry = entry
		}
	}
	if maxLen == 0 {
		return FwdEntry{}, false
	}
	return longestMatchEntry, true
}

// TODO: potentially move these serializers/deserializers to Protocol? idk

/* serializes an IP packet (IPV4 Header + bytes message) for the UDP layer to send */
func SerializePacket(source netip.Addr, dest netip.Addr, message string, protocol int, ttl int, isTtlNew bool) ([]byte) {
	/* Start filling in the header, use passed in fields */
	hdr := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(message),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      32,
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
	bytesToSend = append(bytesToSend, []byte(message)...)

	return bytesToSend
}

/* Takes in raw bytes, validates the checksum, checks TTL, and returns the deserialized message and header */
func DeserializeAndValidatePacket(packet ll.IPPacket) (string, *ipv4header.IPv4Header, error) {
	/* Marshal the received byte array into a UDP header
	NOTE:  This does not validate the checksum or check any fields */
	hdr, err := ipv4header.ParseHeader(packet.Data)

	if err != nil {
		/* drop packet if parsing doesn't work */
		fmt.Println("Error parsing header", err)
		return "", nil, err
	}

	/* extract and validate checksum */
	headerSize := hdr.Len
	headerBytes := packet.Data[:headerSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := utils.ValidateChecksum(headerBytes, checksumFromHeader)

	/* determine if we passed or failed checksum */
	if computedChecksum == checksumFromHeader {
		fmt.Println("Checksum passed")
	} else {
		fmt.Printf("Checksum failed, dropping packet from %s\n", packet.SrcIfaceAddr.String())
		return "", nil, err
	}

	// check TTL
	if hdr.TTL <= 0 {
		fmt.Printf("TTL expired, dropping packet from %s\n", packet.SrcIfaceAddr.String())
		return "", nil, err
	}

	/* Next, get the message, which starts after the header */
	message := packet.Data[headerSize:]

	return string(message), hdr, nil
}