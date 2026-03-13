package ipStack

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
)

/* run the REPL */
func (stack *IPStack) StartREPL() {
	
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println("\nExiting REPL")
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		stack.HandleCommand(line)
	}
}

/* parse user command from REPL */
func (stack *IPStack) HandleCommand(line string) {
	parts := strings.SplitN(line, " ", 3)

	if len(parts) < 1 {
		return
	}

	switch parts[0] {

	case "send":
		/* verify send command format */
		if len(parts) < 3 {
			fmt.Println("Usage: send <ip> <message>")
			return
		}
		ipStr := parts[1]
		msg := parts[2]

		/* verify that second parameter at least looks like a valid IP */
		destIP, err := netip.ParseAddr(ipStr)
		if err != nil {
			fmt.Println("Invalid IP Format: ", ipStr)
			return
		}
		/* CALL IP LAYER SEND! */
		err = stack.SendIP(destIP, msg)
		if err != nil {
			fmt.Println("Send error:", err)
		}
	/* list interfaces */
	case "li":
		stack.ListInterfaces()
	/* list neighbors */
	case "ln":
		stack.ListNeighbors()
	/* list routes */
	case "lr":
		stack.ListRoutes()
	/* disable interface down <ifname> */
	case "down":
		if len(parts) < 2 {
			fmt.Println("Usage: down <if_num>")
			return
		}
		stack.IFDown(parts[1])
	/* enable interface up <ifname> */
	case "up":
		if len(parts) < 2 {
			fmt.Println("Usage: up <if_num>")
			return
		}
		stack.IFUp(parts[1])
	/* not actual requirements */
	case "me":
		stack.PrintInterfacesForDebugging()
	case "q":
		os.Exit(0)
	default:
		fmt.Println("Unknown command")
	}
	fmt.Printf("> ")
}

/* "LI" command */
func (stack *IPStack) ListInterfaces() {
	table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Iface", "VIP", "UDPAddr"})

    for _, iface := range stack.Interfaces {
		UDPAddr := iface.Conn.LocalAddr().String()
        table.Append([]string{
            iface.Name,
			iface.IP.String(),
			UDPAddr,
        })
    }
	/* print out table */
    table.Render()
}

/* "LN" command */
func (stack *IPStack) ListNeighbors() {
	table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Iface", "VIP", "UDPAddr"})
	/* go through each connected interface's neighbors to print all neighbors */
    for _, iface := range stack.Interfaces {
		ifaceName := iface.Name
		for neighborIP, neighbor := range iface.Neighbours {
			table.Append([]string{
				ifaceName,
				neighborIP.String(),
				neighbor.UDPAddr.String(),
			})
		}
    }
	/* print out table */
    table.Render()
}

/* "LR" command */
func (stack *IPStack) ListRoutes() {
	table := tablewriter.NewWriter(os.Stdout)
	table.Header([]string{"T", "Prefix", "Next hop", "Cost"})

	for _, e := range stack.ForwardingTable {
		/* type */
		typeStr := typeToLetter(e.Type)

		/* next hop */
		var nextHopStr string
		if e.Type == SourceTypeLocal {
			nextHopStr = "LOCAL:" + e.InterfaceName
		} else {
			nextHopStr = e.NextHop.String()
		}

		/* cost */
		var costStr string
		if e.Type == SourceTypeStatic {
			costStr = "-" /* default/static routes */
		} else {
			costStr = fmt.Sprint(e.Cost)
		}
		table.Append([]string{
			typeStr,
			e.Prefix.String(),
			nextHopStr,
			costStr,
		})
	}
	table.Render()
}
/* get appropriate letter for table */
func typeToLetter(t int) string {
	switch t {
	case SourceTypeLocal:
		return "L"
	case SourceTypeStatic:
		return "S"
	case SourceTypeRIP:
		return "R"
	default:
		return "?"
	}
}

/* Print the Interface table DEBUGGING */
func (stack *IPStack) PrintInterfacesForDebugging() {
    table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Name", "My IP on this IF", "Prefix", "Neighbors"})

    for _, iface := range stack.Interfaces {
        ns := ""
        for addr := range iface.Neighbours {
            ns += addr.String() + "\n"
        }
        table.Append([]string{
            iface.Name,
			iface.IP.String(),
            iface.Prefix.String(),
            ns,
        })
    }
	/* print out table */
    table.Render()
}

/* Print Forwarding Table DEBUGGING */
func (stack *IPStack) PrintForwardingTable() {
    table := tablewriter.NewWriter(os.Stdout)
    table.Header([]string{"Prefix", "NextHop", "Iface", "Type", "Cost"})

    for _, e := range stack.ForwardingTable {
		ifaceStr := "-"
		if e.InterfaceName != "" {
			ifaceStr = e.InterfaceName
		}

        table.Append([]string{
            e.Prefix.String(),
            e.NextHop.String(),
            ifaceStr, /* may not be there */
            fmt.Sprint(e.Type),
			fmt.Sprint(e.Cost),
        })
    }
    table.Render()
}
