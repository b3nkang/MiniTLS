package ipStack

import (
	"errors"
	"fmt"
	"ip-isabelle-and-ben/pkg/lnxconfig"
	"log"
	"net"
	"net/netip"
)

/* Not a method *ON* IPstack, but shared between host and router */
func InitIPStackFromConfig(fileName string)(*IPStack, error) {
	/* parse lnx file */
	lnxConfig, err := lnxconfig.ParseConfig(fileName)
	if err != nil {
		return nil, err
	}
	
	ipStack := &IPStack{
		Interfaces:      make(map[string]*Interface),
		ForwardingTable: make(map[netip.Prefix]FwdEntry, 0),
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
		iface := &Interface{
			Name: ifx.Name,
			Prefix: ifx.AssignedPrefix,
			IP: ifx.AssignedIP,
			Conn: conn,
			Neighbours: make(map[netip.Addr]Neighbour),
		}
		
		stack.Interfaces[ifx.Name] = iface
	}

	/* Initialize Neighbors */
	for _, n := range config.Neighbors {
		iface, ok := stack.Interfaces[n.InterfaceName]
		if !ok {
			log.Printf("Neighbor %s contains unknown interface: %s", n.DestAddr, n.InterfaceName)
		}
		neighbor := Neighbour{
			IP: n.DestAddr, 	/* netip.Addr */
			UDPAddr: n.UDPAddr, /* net.UDPAddr */
		}
		/* map neighbor's IP address to neighbor struct */
		iface.Neighbours[n.DestAddr] = neighbor
	}

	err := stack.initFwdTable(config)
	if err != nil {
		return err
	}

	/* start listeners for each interface */
	for _, iface := range stack.Interfaces {
		go LinkLayerListen(iface.Conn, stack.IncomingPackets)
	}

	return nil
}

/* Initialize forwarding table based on RoutingMode */
func (stack *IPStack) initFwdTable(config *lnxconfig.IPConfig) error {
	switch config.RoutingMode {
		/* case: host */
		case lnxconfig.RoutingTypeStatic:
			for _, iface := range stack.Interfaces {
				entry := FwdEntry{
					Prefix: iface.Prefix,
					NextHop: netip.Addr{}, /* empty = LOCAL */
					InterfaceName: iface.Name,
					/* no need to define Type (not a router) */
					/* no need to define Cost (not a router) */
				}
				stack.ForwardingTable[iface.Prefix] = entry
			}
		/* case: router */
		case lnxconfig.RoutingTypeRIP:
			for _, iface := range stack.Interfaces {
				/* we only know about directly connected interfaces currently */
				entry := FwdEntry{
					Prefix: iface.Prefix,
					NextHop: netip.Addr{}, /* empty = LOCAL */
					InterfaceName: iface.Name,
					Type: SourceTypeDirect,
					Cost: 0, /* all local interfaces have cost = 0*/
				}
				stack.ForwardingTable[iface.Prefix] = entry
			}
			/* TODO: call a function to setup RIP stuff maybe */

		default:
			log.Printf("Routing Mode that's not static or RIP: %d\n", config.RoutingMode)
			return errors.New("Incorrect Routing Mode")
		}
	return nil
}


/* Run the IP Layer (handle and process messages) */
func (stack *IPStack) RunIPLayer() {
	for packet := range stack.IncomingPackets {
		fmt.Printf("IP Layer got this packet too: %s\nHeader:  %v\nChecksum:  %s\nMessage:  %s\n",
			packet.Header.Src.String(), packet.Header, packet.Header.Checksum, string(packet.Data))
	}
}

/* Send a message on the IP Layer HARDCODED H1 -> R1 */
/*
	Test: send message from h1 to r1 in doc-example
	h1: 
		- UDP: 127.0.0.1:5000
	r1: 
		- IP to send to: 10.0.0.2 
		- UDP to send to: 127.0.0.1:5001
*/
func (stack *IPStack) SendIP(dest netip.Addr, message string) error {
	/* hardcode fields that will come from forwarding and neighbor table for now */

	/* conn for this stack's if0 (assume h1) */
	myConn := stack.Interfaces["if0"].Conn

	/* string for r1's IP and port */
	destStringAddr := "127.0.0.1:5001"

	destUDPAddr, err := net.ResolveUDPAddr("udp", destStringAddr)
	if err != nil {
		panic(err)
	}
	sourceIP := stack.Interfaces["if0"].IP
	
	LinkLayerSend(myConn, destUDPAddr, sourceIP, dest, message, 0)

	return nil
}





