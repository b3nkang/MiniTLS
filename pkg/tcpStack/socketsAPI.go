package tcpstack

import (
	"errors"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
)

/* init socket table and TCP stack */
func InitTCPStack(ipStack *ipStack.IPStack) *TCPStack {
	table := &SocketTable{
		socketMap: make(map[int]*SocketTableEntry),
		nextID: 0,
	}
	tcp := &TCPStack{
		socketTable: table,
		ipStack: ipStack,
	}
	return tcp
}

/* create new listening socket bound to specified port */
func (tcp *TCPStack) VListen(port uint16) (*VTCPListener, error) {
	listener := &VTCPListener{
		port: port,
		/* second param represents size of queue waiting to be sent in channel
			so we allow up to 5 connections to wait in a "queue" in case
			the program calls Accept() again */
		connChan: make(chan *VTCPConn, 5),
		acceptingConns: false,
	}

	/* add listen socket to table */
	tableEntry := &SocketTableEntry{
		localPort:	listener.port,
		state: 	 	LISTEN,
		socketID:   tcp.socketTable.nextID,
		listenSocket: listener,
	}

	/* increment next ID for next entry */
	tcp.socketTable.nextID++
	/* add this entry to table */
	tcp.socketTable.socketMap[tableEntry.socketID] = tableEntry
	fmt.Printf("Made a new entry and listener socket in host's socket table with: \n Port: %d\nState: %d\nSocketID: %d\n",
				tableEntry.localPort, tableEntry.state, tableEntry.socketID)
	tcp.socketTable.listSockets()
	return listener, nil
}

/* listen for new connections until someone connects */
func (listener *VTCPListener) VAccept() (*VTCPConn, error) {
	listener.acceptingConns = true
	conn := <-listener.connChan
	return conn, nil
}

/* create a new conn and perform handshake. block until connection established */
func (tcp *TCPStack) VConnect(addr netip.Addr, port uint16) (*VTCPConn, error) {
	table := tcp.socketTable

    conn := &VTCPConn{
        packetChan: make(chan []byte),
    }

	/* ensure that ephemeral port generated is unique--keep generating new ports until
		we get one (should only take 1 try) */
	var srcPort uint16
	for {
		srcPort = utils.RandomEphemeralPort()
		if table.portIsUnique(srcPort) {
			break
		}
	}
	fmt.Printf("[TCP] Port generated in VConnect: %d\n", srcPort)
	/* get this conn's local IP (just going to use if0?) */
	localInterface := tcp.ipStack.Interfaces["if0"]
	localIP := localInterface.IP

	/* make new socket table entry */
    entry := &SocketTableEntry{
        localPort: srcPort,
		localIP: localIP,
        destPort: port,
        destIP: addr,
        state: SYN_SENT,
        socketID: table.nextID,
        normalSocket: conn,
		seqNum: utils.GenerateNewSeq(),
        establishedChan: make(chan int, 1),
    }
	/* make sure we increment table's next port! */
    table.nextID++
    table.socketMap[entry.socketID] = entry

	fmt.Println("[TCP] sending SYN from VConnect")

	/* send SYN */
	tcp.sendSyn(entry)

    // block until state changes
    for {
        state := <-entry.establishedChan
        switch state {
		case ESTABLISHED:
            return conn, nil
		/* TODO: actually send this situation if there is an error in the 3-way handshake */
        case ERROR:
			return nil, errors.New("Error ocurred before 3-way handshake concluded")
		}
    }
}

/* returns bytes written (or error) */
func (conn *VTCPConn) VWrite(data []byte) (int, error) {
	/* give the data to the conn's sendbuf data channel */
	conn.sendBuf.mu.Lock()
	defer conn.sendBuf.mu.Unlock()

	buf := conn.sendBuf

	/* write data to buffer--only as much as fits */
	spaceInBuf := MAX_WIN_SIZE - (buf.lbw + 1) /* say last byte written is 0 and size is 5: 5-0 = 5 but we want 4 so (lbw + 1) */
	if spaceInBuf <= 0 {
		fmt.Println("No space in buffer. Returning for now...eventually deal with this")
		return 0, nil
	}

	/* truncate bytes to fit in buffer if necessary */
	numBytesToWrite := len(data)
	if numBytesToWrite > int(spaceInBuf) {
		numBytesToWrite = int(spaceInBuf)
	}

	/* write bytes to buffer */
	/* NOTE: when we switch to circular buffer, we cannot do this--need a loop to write one byte at a time -- should 
		just make that a function of the circular buffer struct though */
	copy(buf.buf[buf.lbw + 1:], data[:numBytesToWrite])

	/* tell sending thread we put stuff in buffer 
		apparently (according to chat, we want this kinda weird structure)
		this will only signal to this channel if it ISN'T full
		if we did: buf.dataWrittenToBuf <- struct{}{} without the select/case/default situation,
		it would block and we'd hold this mutex. if the channel is already full
		i.e. someone else wrote to it, we could cause deadlock.
		if the channel is full, the sender will check the buffer anyway and our data will be sent.
		at least that's the idea...? */
	select {
	case buf.dataWrittenToBuf <- struct{}{}:
	default:
	}

	return numBytesToWrite, nil
}

// TODO: returning numBytesRead for now but check if right
func (conn *VTCPConn) VRead(buf []byte) (int, error) {
    // block tiill data
    data := <-conn.recvBuf.dataToRead

    conn.recvBuf.mu.Lock()
    defer conn.recvBuf.mu.Unlock()

    numBytesRead := copy(buf, data)
    conn.recvBuf.lbr += uint32(numBytesRead)
    conn.recvBuf.currSize -= uint32(numBytesRead)

    return numBytesRead, nil
}

/* verify that random port doesn't conflict with existing connection in table */
func (table *SocketTable) portIsUnique(newPort uint16) bool {
	for _, entry := range table.socketMap {
		if entry.localPort == newPort {
			return false
		}
	}
	return true
}

