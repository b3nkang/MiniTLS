package tcpstack

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
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

			// TODO::: bug when port doesn't exist -
			// panic: runtime error: invalid memory address or nil pointer dereference
			// [segmentation violation code=0x1 addr=0x40 pc=0x154a0c]
			// goroutine 8 [running]:
			// ip-isabelle-and-ben/pkg/tcpStack.(*TCPStack).HandleTCP(0x4000011440, 0x40003260a0, {0x40000a2014, 0x14, 0x564})
			// 		/home/cs1680-user/tcp-big-chungus-1/pkg/tcpStack/doTcp.go:85 +0x46c
			// ip-isabelle-and-ben/pkg/ipStack.(*IPStack).ipForwarding(0x400007a4e0, 0x40003260a0, {0x40000a2014, 0x14, 0x5
		case "s":
			if len(parts) != 3 {
				fmt.Println("Usage: s <socketID> <bytes>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}
			go tcp.sCommand(sID, []byte(parts[2]))
		case "r":
			if len(parts) != 3 {
				fmt.Println("Usage: r <socketID> <numbytes>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}
			numBytes, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Println("numbytes must be an integer")
				continue
			}
			go tcp.rCommand(sID, numBytes)
		case "ls":
			tcp.socketTable.listSockets()
		case "ps":
			if len(parts) != 2 {
				fmt.Println("Usage: ps <socketID>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}
			tcp.pSendBuf(sID)
		case "pr":
			if len(parts) != 2 {
				fmt.Println("Usage: pr <socketID>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}
			tcp.pRecvBuf(sID)
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

	/* should we be looping forever? idk what to do here really... */
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

/* call VWrite after finding correct socket entry in table */
func (tcp *TCPStack) sCommand(socketNum int, data []byte) {
	socket := tcp.getNormalSocket(socketNum)
	/* call VWrite */
	bytesWritten, err := socket.VWrite(data)

	if err == nil {
		fmt.Printf("%d bytes written to socket %d\n", bytesWritten, socketNum)
		return
	} else {
		fmt.Printf("Error with VWrite: %s", err)
		return
	}
}

/* call VRead after locating socket */
func (tcp *TCPStack) rCommand(socketNum int, numBytes int) {
	socket := tcp.getNormalSocket(socketNum)
    buf := make([]byte, numBytes)
    bytesCopied, err := socket.VRead(buf)
    if err != nil {
        fmt.Printf("VRead error: %s\n", err)
        return
    }

    fmt.Printf("Read %d bytes: %s\n", bytesCopied, string(buf[:bytesCopied]))
}

/* list socket table */
func (table *SocketTable) listSockets() {
	// table.mu.Lock()
	// defer table.mu.Unlock()

	tw := tablewriter.NewWriter(os.Stdout)

	tw.Header([]string{
		"SID",
		"LAddr",
		"LPort",
		"RAddr",
		"RPort",
		"Status",
	})

	for _, e := range table.socketMap {

		laddr := addrToString(e.localIP)
		raddr := addrToString(e.destIP)

		lport := fmt.Sprint(e.localPort)
		rport := fmt.Sprint(e.destPort)

		stateStr := stateToString(e.state)

		tw.Append([]string{
			fmt.Sprint(e.socketID),
			laddr,
			lport,
			raddr,
			rport,
			stateStr,
		})
	}
	tw.Render()
}

/* for printing Socket Table */
func stateToString(s int) string {
	switch s {
	case LISTEN:
		return "LISTEN"
	case SYN_SENT:
		return "SYN-SENT"
	case SYN_RECEIVED:
		return "SYN-RECVD"
	case ESTABLISHED:
		return "ESTABLISHED"
	default:
		return "?"
	}
}

/* for printing Socket Table: want '0.0.0.0' if IP is not set */
func addrToString(a netip.Addr) string {
	if !a.IsValid() {
		return "0.0.0.0"
	}
	return a.String()
}

/* print socket table entry for debugging */
func PrintSocketTableEntry(e *SocketTableEntry) {
	tw := tablewriter.NewWriter(os.Stdout)
	fmt.Println("[TCP PRINT] Printing socket table entry")

	tw.Header([]string{"Field", "Value"})

	/* deal with undefined stuff */
	ptrStr := func(p any) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprintf("%p", p)
	}

	/* print channels */
	chanStr := func(c chan int) string {
		if c == nil {
			return "nil"
		}
		return fmt.Sprintf("%p", c)
	}

	tw.Append([]string{"socketID", fmt.Sprint(e.socketID)})
	tw.Append([]string{"state", stateToString(e.state)})

	tw.Append([]string{"localIP", addrToString(e.localIP)})
	tw.Append([]string{"localPort", fmt.Sprint(e.localPort)})

	tw.Append([]string{"destIP", addrToString(e.destIP)})
	tw.Append([]string{"destPort", fmt.Sprint(e.destPort)})

	tw.Append([]string{"seqNum", fmt.Sprint(e.seqNum)})

	tw.Append([]string{"normalSocket", ptrStr(e.normalSocket)})
	tw.Append([]string{"listenSocket", ptrStr(e.listenSocket)})

	tw.Append([]string{"establishedChan", chanStr(e.establishedChan)})

	tw.Render()
}

func (tcp *TCPStack) getNormalSocket(socketNum int) (*VTCPConn) {
    tcp.socketTable.mu.Lock()
    socket, exists := tcp.socketTable.socketMap[socketNum]
    tcp.socketTable.mu.Unlock()

	if !exists {
		fmt.Println("Invalid socket ID number")
		return nil
	}
	/* check that normal socket exists */
	if socket.normalSocket == nil {
		fmt.Println("Socket table does not have TCPConn associated yet")
		return nil
	}
	/* check that conn is established */
	if socket.state != ESTABLISHED {
		fmt.Printf("Connection with %d not ESTABLISHED\n", socketNum)
		return nil
	}

	return socket.normalSocket
}

func (tcp *TCPStack) pSendBuf(socketNum int) {
	socket := tcp.getNormalSocket(socketNum)
	if socket == nil {
		return
	}

	sendBuf := socket.sendBuf
	sendBuf.mu.Lock()
	defer sendBuf.mu.Unlock()

	printBufferWithPointers(sendBuf.buf, sendBuf.base, 10, []BufPointer{
		{seq: sendBuf.una, mark: "U"},
		{seq: sendBuf.nxt, mark: "N"},
		{seq: sendBuf.lbw, mark: "L"},
	})
}

func (tcp *TCPStack) pRecvBuf(socketNum int) {
	socket := tcp.getNormalSocket(socketNum)
	if socket == nil {
		return
	}

	recvBuf := socket.recvBuf
	recvBuf.mu.Lock()
	defer recvBuf.mu.Unlock()

	printBufferWithPointers(recvBuf.buf, recvBuf.base, 10, []BufPointer{
		{seq: recvBuf.lbr, mark: "R"},
		{seq: recvBuf.nxt, mark: "N"},
	})
}
