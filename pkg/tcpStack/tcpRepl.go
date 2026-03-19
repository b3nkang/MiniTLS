package tcpstack

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

/* deal with REPL commands from IP REPL */
func (tcp *TCPStack) HandleREPLCommands() {
	/* this blocks on channel waiting for commands */
	for cmd := range tcp.ipStack.TCPReplChan {
		parts := strings.Split(cmd, " ")
		switch parts[0] {

		/* accept: a <portNum> */
		case "a":
			fmt.Println("a command recognized in TCP")
			if len(parts) < 2 {
				fmt.Println("Usage: a <portNum>")
				continue
			}
			portInt, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Port must be an integer")
				continue
			}
			if portInt < 0 || portInt > 65535 {
				fmt.Println("Port must be a number 0-65535")
				continue
			}
			port := uint16(portInt)
			go tcp.aCommand(port) // so REPL dont block
		case "c":
			/* connect */
			fmt.Println("c command recognized in TCP")
			if len(parts) != 3 {
				fmt.Println("Usage: c <vip> <port>")
				continue
			}
			addr, err := netip.ParseAddr(parts[1])
			if err != nil {
				fmt.Println("Invalid IP Format: ", parts[1])
				continue
			}
			portInt, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Println("Port must be an integer")
				continue
			}
			if portInt < 0 || portInt > 65535 {
				fmt.Println("Port must be a number 0-65535")
				continue
			}
			port := uint16(portInt)
			go tcp.cCommand(addr, port) // so REPL dont block
		default:
			fmt.Println("Unknown TCP command")
		}
	}
}

func (tcp *TCPStack) aCommand(port uint16) {
	/* call VListen to make new Listner socket */
	listener, err := tcp.VListen(port)
	if err != nil {
		fmt.Println("Listen error:", err)
		return
	}

	/* idk what to do here really... */
	for {
		fmt.Println("calling Accept, this will just block for now and not return")
		conn, err := listener.VAccept()
		if err != nil {
			fmt.Println("Accept error:", err)
			return
		}
		fmt.Println("Accepted connection", conn)
	}
}

func (tcp *TCPStack) cCommand(addr netip.Addr, port uint16) {
    conn, err := tcp.VConnect(addr, port)
    if err != nil {
        fmt.Println("Connect error:", err)
        return
    }
    fmt.Println("Connected!", conn)
}