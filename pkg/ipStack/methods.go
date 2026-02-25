package ipStack

import (
	"errors"
	"fmt"
	"ip-isabelle-and-ben/pkg/lnxconfig"
	"log"
	"net"
	"net/netip"
	"os"

	"github.com/olekukonko/tablewriter"
)

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

/* Print the Interface table */
func (stack *IPStack) PrintInterfaces() {
    table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Name", "Prefix", "Neighbors"})

    for _, iface := range stack.Interfaces {
        ns := ""
        for addr := range iface.Neighbours {
            ns += addr.String() + "\n"
        }
        table.Append([]string{
            iface.Name,
            iface.Prefix.String(),
            ns,
        })
    }
	/* print out table */
    table.Render()
}

/* Print Forwarding Table */
func (stack *IPStack) PrintForwardingTable() {
    table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Prefix", "NextHop", "Iface", "Type", "Cost"})

    for _, e := range stack.ForwardingTable {
        table.Append([]string{
            e.Prefix.String(),
            e.NextHop.String(),
            e.InterfaceName,
            fmt.Sprint(e.Type),
			fmt.Sprint(e.Cost),
        })
    }
    table.Render()
}


