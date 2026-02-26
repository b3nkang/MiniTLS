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
		stack.handleCommand(line)
	}
}

/* parse user command from REPL */
func (stack *IPStack) handleCommand(line string) {
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
	/* edit based on what the actual "quit" requiremet is */
	case "q":
		os.Exit(0)

	default:
		fmt.Println("Unknown command")
	}
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
