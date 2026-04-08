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
		sendRequests: make(chan *SendRequest, 100),
	}
	go tcp.sendPacketsOut()
	/* make a loop that sends packets out as they come in */
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
	fmt.Printf("Listening on Port: %d, SocketID: %d\n",
				tableEntry.localPort, tableEntry.socketID)
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
	
	/* set the function to send packets here while we have access to tcp stack -- conn will not when it's trying to send */
	entry.sendPacketFunc = func(sendReq *SendRequest) {
		tcp.sendRequests <- sendReq
	}

	fmt.Println("[TCP] sending SYN from VConnect")

	/* send SYN */
	entry.sendSyn()

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

	sendBuf := conn.sendBuf

	/* write data to buffer--only as much as fits */
	// /* TODO: CHANGE THIS FOR CIRCULAR BUF */
	// spaceInBuf := MAX_WIN_SIZE - (buf.lbw + 1) /* say last byte written is 0 and size is 5: 5-0 = 5 but we want 4 so (lbw + 1) */
	// if spaceInBuf <= 0 {
	// 	fmt.Println("No space in buffer. Returning for now...eventually deal with this")
	// 	return 0, nil
	// }
	spaceInBuf := sendBuf.cBuf.FreeSpace()
	if spaceInBuf == 0 {
		fmt.Println("[TCP - VWrite] no space in send buffer")
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

	fmt.Printf("[TCP - VWrite] send buf size before write: %d\n", sendBuf.cBuf.currSize)
		
	// // // Old pre-circbuf
	// // start := int(buf.lbw + 1 - buf.base)
	// // copy(buf.buf[start:], data[:numBytesToWrite])
	// // buf.currSize += uint32(numBytesToWrite)
	// /* update lbw */
	// buf.lbw += uint32(numBytesToWrite) /* TODO: update with circular buffer */
	sendBuf.cBuf.WriteIntoBuf(sendBuf.lbw+1, data[:numBytesToWrite])
	start:= sendBuf.lbw+1
	sendBuf.lbw += uint32(numBytesToWrite)

	fmt.Printf("[TCP - VWrite] send buf size after write: %d\n",sendBuf.cBuf.currSize)
	fmt.Printf("[TCP - VWrite] data written to send buf: %q\n",sendBuf.cBuf.SliceFrom(start, uint32(numBytesToWrite)))

	/* tell sending thread we put stuff in buffer 
		apparently (according to chat, we want this kinda weird structure)
		this will only signal to this channel if it ISN'T full
		if we did: buf.dataWrittenToBuf <- struct{}{} without the select/case/default situation,
		it would block and we'd hold this mutex. if the channel is already full
		i.e. someone else wrote to it, we could cause deadlock.
		if the channel is full, the sender will check the buffer anyway and our data will be sent.
		at least that's the idea...? */
	select {
	case sendBuf.dataWrittenToBuf <- struct{}{}:
	default:
	}

	return numBytesToWrite, nil
}

// TODO: returning numBytesRead for now but check if right -- that is right
func (conn *VTCPConn) VRead(buf []byte) (int, error) {
	/* loop so that we block until data is ready */
    for {
		/* lock mutex before accessing fields */
        conn.recvBuf.mu.Lock()
		/* if current size == 0, no new data in buffer, need to wait for signal to read */
        if conn.recvBuf.cBuf.currSize == 0 {
            conn.recvBuf.mu.Unlock()
            <-conn.recvBuf.dataToRead  /* wait for signal that new data exists */
            continue
        }
        /* otherwise, we know there is data ready and we can just read it */
		numBytesToRead := len(buf)
		if numBytesToRead > int(conn.recvBuf.cBuf.currSize) {
			numBytesToRead = int(conn.recvBuf.cBuf.currSize)
		}

		numBytesRead := conn.recvBuf.cBuf.ReadFromBuf(conn.recvBuf.lbr+1, buf, uint32(numBytesToRead))
		conn.recvBuf.lbr += uint32(numBytesRead)
		conn.recvBuf.cBuf.AdvanceBase(uint32(numBytesRead))

		// // old pre-circbuf
		// /* find place to read from: last byte read - base + 1 */
		// readStart := conn.recvBuf.lbr - conn.recvBuf.base + 1
		// /* copy to passed in buffer */
		// numBytesRead := copy(buf, conn.recvBuf.buf[readStart:readStart+conn.recvBuf.currSize])
		// /* update lbr */
		// conn.recvBuf.lbr += uint32(numBytesRead)
		// /* decrease current size (more important for circular buffer */
		// conn.recvBuf.currSize -= uint32(numBytesRead)

        conn.recvBuf.mu.Unlock()
        return numBytesRead, nil
    }
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

