package tcpstack

import (
	"fmt"
	"io"
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
		/* receive file */
		case "rf":
			if len(parts) != 3 {
				fmt.Println("Usage: rf <destFilePath> <portNum>")
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
			go tcp.rfCommand(parts[1], portInt)
		/* send file sf path/to/some_file 10.1.0.2 9999 */
		case "sf":
			if len(parts) != 4 {
				fmt.Println("Usage: rf <srcFilePath> <Receiver IP> <Receiver PortNum>")
				continue
			}
			portInt, err := strconv.Atoi(parts[3])
			if err != nil {
				fmt.Println("Port must be an integer")
				continue
			}
			if portInt < 0 || portInt > 65535 {
				fmt.Println("Port must be a number 0-65535")
				continue
			}
			addr, err := netip.ParseAddr(parts[1])
			if err != nil {
				fmt.Println("Invalid IP Format: ", parts[1])
				continue
			}
			go tcp.sfCommand(parts[1], addr, portInt)
		case "ls":
			tcp.socketTable.listSockets()
		case "d":
			if len(parts) != 2 {
				fmt.Println("Usage: d <socketID>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}			
			tcp.dCommand(sID)
		case "prq":
			if len(parts) != 2 {
				fmt.Println("Usage: prq <socketID>")
				continue
			}
			sID, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Socket ID must be an integer")
				continue
			}			
			tcp.prqCommand(sID)
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

	for {
		_, err := listener.VAccept()
		if err != nil {
			fmt.Println("Accept error:", err)
			return
		}
	}
}

func (tcp *TCPStack) cCommand(addr netip.Addr, port uint16) {
    _, err := tcp.VConnect(addr, port)
    if err != nil {
        fmt.Println("Connect error:", err)
        return
    }
}

/* call VWrite after finding correct socket entry in table */
func (tcp *TCPStack) sCommand(socketNum int, data []byte) {
	socket := tcp.getNormalSocket(socketNum)
	if socket == nil {
		fmt.Printf("Cannot send to socket: %d\n", socketNum)
		return
	}
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
	if socket == nil {
		fmt.Printf("Cannot read from socket: %d\n", socketNum)
		return
	}
    buf := make([]byte, numBytes)
    bytesCopied, err := socket.VRead(buf)
    if err != nil {
        fmt.Printf("VRead error: %s\n", err)
        return
    }

    fmt.Printf("Read %d bytes: %s\n", bytesCopied, string(buf[:bytesCopied]))
}

/* receive file */
/* rf path/to/some_destination_file 9999 */
/* WILL NOT WORK because we don't return io.EOF when we get FIN */
func (tcp *TCPStack) rfCommand(destFile string, portNum int) {
	/* create listener */
	listener, err := tcp.VListen(uint16(portNum))
	if err != nil {
		fmt.Println("Listen error:", err)
		return
	}

	/* accept connection */
	conn, err := listener.VAccept()
	if err != nil {
		fmt.Println("Accept error:", err)
		return
	}

	/* make or open file */
	file, err := os.Create(destFile)
	if err != nil {
		fmt.Println("Error creating file: ", err)
		return
	}

	/* close file eventually */
	defer file.Close()

	/* read one page at a time */
	buf := make([]byte, 4096)
	
	/* read file data sent to conn 
		if VRead blocks until data is ready, how will 
		we ever know if it's done?
	*/
	for {
		/* read into buf */
		numBytesRead, err := conn.VRead(buf)

		/* check for EOF -> Read done (may need to move after writing data if we're
			mirroring Go's pattern here) */
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("Read error: ", err)
			return
		}

		/* if we got data, put it in buffer */
		if numBytesRead > 0 {
			_, writeErr := file.Write(buf[:numBytesRead])
			if writeErr != nil {
				fmt.Println("File write error: ", writeErr)
				return
			}
		}
	}

}

/* write file */
/* sf path/to/some_file 10.1.0.2 9999 */
func (tcp *TCPStack) sfCommand(srcFile string, addr netip.Addr, portNum int) {
	/* connect to receiver */
    conn, err := tcp.VConnect(addr, uint16(portNum))
    if err != nil {
        fmt.Println("Connect error:", err)
        return
    }

	/* write file */
	file, err := os.Open(srcFile)
	if err != nil {
        fmt.Println("Open file error:", err)
        return
    }
	defer file.Close()

	/* need a buffer to read from file -> put that buf in VWrite */
	buf := make([]byte, 4096) /* page size */
	for {
		bytesRead, readErr := file.Read(buf)

		if readErr != nil && readErr != io.EOF {
			fmt.Println("File read error: ", err)
			return
		}
		if bytesRead > 0 {
			bytesWritten, writeErr := conn.VWrite(buf)
			if writeErr != nil {
				fmt.Println("VWrite error:", writeErr)
                return
			}
			if bytesWritten != bytesRead {
				fmt.Printf("Error with VWrite: wrote only %d bytes instead of the full %d bytes read\n", bytesWritten, bytesRead)
				return
			}
		}
		/* written whole file -> check AFTER read because apparently in Go
			Read() can return error EOF and bytes read */
		if readErr == io.EOF {
			break
		}
	}

	/* TODO: close connection */
	/* conn.VClose() */
	return
}

// command for testing retransmissions by configuring the host to drop all packets in handlePayload()
func (tcp *TCPStack) dCommand(socketNum int) {
	entry := tcp.socketTable.socketMap[socketNum]
	if entry == nil {
		fmt.Printf("Cannot read from socket: %d\n", socketNum)
		return
	}
	if entry.dropForRetrans {
		fmt.Printf("set entry.dropForRetrans = FALSE, accepting packets now\n")
		entry.dropForRetrans = false
	} else {
		fmt.Printf("set entry.dropForRetrans = TRUE, dropping packets now \n")
		entry.dropForRetrans = true
	}
}

// prq = print retransmission queue
func (tcp *TCPStack) prqCommand(socketNum int) {
	conn := tcp.getNormalSocket(socketNum)
	array := conn.retransQueue.array
	seqs := make([]uint32, 0, len(array))
	for _, entry := range array {
		if entry != nil {
			seqs = append(seqs, entry.seqNum)
		} else {
			seqs = append(seqs, 0) // placeholder
		}
	}
	fmt.Println(seqs)
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

// NOTE: these no longer work, was for testing with simple arrays

func (tcp *TCPStack) pSendBuf(socketNum int) {
	socket := tcp.getNormalSocket(socketNum)
	if socket == nil {
		return
	}

	sendBuf := socket.sendBuf
	sendBuf.mu.Lock()
	defer sendBuf.mu.Unlock()

	printBufferWithPointers(sendBuf.cBuf.buf, sendBuf.base, 10, []BufPointer{
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

	printBufferWithPointers(recvBuf.cBuf.buf, recvBuf.cBuf.baseSeq, 10, []BufPointer{
		{seq: recvBuf.lbr, mark: "R"},
		{seq: recvBuf.nxt, mark: "N"},
	})
}
