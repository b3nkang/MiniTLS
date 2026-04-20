package tcpstack

import (
	"errors"
	"fmt"
	"io"
	"ip-isabelle-and-ben/pkg/ipStack"
	utils "ip-isabelle-and-ben/pkg/protocol"
	"net/netip"
	"time"
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
		removeSelf: tcp.socketTable.Remove,
	}

	listener.socketEntry = tableEntry

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
	fmt.Printf("New connection => created new socket %d\n", conn.socketID)
	return conn, nil
}

/* create a new conn and perform handshake. block until connection established
TODO: if we put in the right IP address but wrong port, we segfault */
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
	conn.socketID = entry.socketID

	conn.socketEntry = entry

	/* set set-removal function */
	entry.removeSelf = tcp.socketTable.Remove
	
	/* set the function to send packets here while we have access to tcp stack -- conn will not when it's trying to send */
	entry.sendPacketFunc = func(sendReq *SendRequest) {
		tcp.sendRequests <- sendReq
	}

	/* send SYN */
	entry.sendSyn()

    // block until state changes
    for {
        state := <-entry.establishedChan
        switch state {
		case ESTABLISHED:
			fmt.Printf("Created new socket with ID %d\n", conn.socketID)
            return conn, nil
		/* TODO: actually send this situation if there is an error in the 3-way handshake */
        case ERROR:
			return nil, errors.New("Error ocurred before 3-way handshake concluded")
		}
    }
}

/* returns bytes written (or error) -- block until all bytes written */
func (conn *VTCPConn) VWrite(data []byte) (int, error) {
	/* check that we are in a state that can write */
	state := conn.socketEntry.state
	if state == FIN_WAIT_1 ||  state == FIN_WAIT_2 || state == TIME_WAIT || state == LAST_ACK || state == CLOSED {
		return 0, errors.New("Connection closing")
	}

	/* give the data to the conn's sendbuf data channel */
	sendBuf := conn.sendBuf
	totalBytesWritten := 0

	/* keep track of how long we need to keep looping (wait for more space in buf as necessary) */
	for totalBytesWritten < len(data) {
		conn.sendBuf.mu.Lock()
		spaceInBuf := sendBuf.cBuf.FreeSpace()

		/* Per handout: This method MUST block until all bytes are in the send buffer.
		If the send buffer becomes full, VWrite should block until space is available. */
		if spaceInBuf == 0 {
			/* unlock mutex so buf can be filled */
			sendBuf.mu.Unlock()
			fmt.Println("[TCP - VWrite] no space in send buffer, waiting for space to be available")
			<- sendBuf.spaceAvailable /* block on this channel */
			continue
		}
		
		/* truncate bytes to fit in buffer if necessary */
		numBytesToWrite := len(data) - totalBytesWritten
		if numBytesToWrite > int(spaceInBuf) {
			numBytesToWrite = int(spaceInBuf)
		}

		startOffset := totalBytesWritten
		endOffset := totalBytesWritten + numBytesToWrite

		fmt.Printf("[TCP - VWrite] send buf size before write: %d\n", sendBuf.cBuf.currSize)

		sendBuf.cBuf.WriteIntoBuf(sendBuf.lbw+1,data[startOffset:endOffset])
		start := sendBuf.lbw+1
		sendBuf.lbw += uint32(numBytesToWrite)
		totalBytesWritten += numBytesToWrite

		sendBuf.mu.Unlock()

		fmt.Printf("[TCP - VWrite] send buf size after write: %d\n",sendBuf.cBuf.currSize)
		fmt.Printf("[TCP - VWrite] data written to send buf: %q\n",sendBuf.cBuf.SliceFrom(start, uint32(numBytesToWrite)))

		select {
		case sendBuf.dataWrittenToBuf <- struct{}{}:
		default:
		}
	}

	return totalBytesWritten, nil
}

// TODO: returning numBytesRead for now but check if right -- that is right
/* TODO : "VRead MUST return number of bytes read into the buffer. 
The returned error is nil on success, io.EOF if other side of 
connection has finished, or another error describing other failure cases.
*/
func (conn *VTCPConn) VRead(buf []byte) (int, error) {
	state := conn.socketEntry.state
	/* check that we are in ESTABLISHED state */
	if state == FIN_WAIT_1 ||  state == FIN_WAIT_2 || state == TIME_WAIT || state == LAST_ACK || state == CLOSED {
		return 0, errors.New("Connection closing")
	}

	/* loop so that we block until data is ready */
    for {
		/* lock mutex before accessing fields */
        conn.recvBuf.mu.Lock()
		/* if current size == 0, no new data in buffer, need to wait for signal to read */
        if conn.recvBuf.cBuf.currSize == 0 {
			/* if state is CLOSE_WAIT -> other side is done sending, so we can send EOF */
			if conn.socketEntry.state == CLOSE_WAIT {
				conn.recvBuf.mu.Unlock()
				return 0, io.EOF
			}
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

        conn.recvBuf.mu.Unlock()
        return numBytesRead, nil
    }
}

func (listener *VTCPListener) VClose() error {
	entry := listener.socketEntry
	if entry.state != LISTEN {
		return errors.New("connection closing")
	}
	/* check if listen socket: */
	if entry.state == LISTEN {
		listener := entry.listenSocket
		if !listener.acceptingConns {
			return errors.New("connection already closed")
		}
		listener.acceptingConns = false
		entry.state = CLOSED
		/* tell socket table to remove us, enter CLOSED state, and return  */
		entry.listenerTeardown()
	}
	return nil
}

/* should be a function on *VTCPListener or *VTCPConn but we can't rlly do that because sendFIN needs 
	entry and so does removing listen socket from table -> solved by adding socketEntry pointer
	should error if listen socket is already closed...but like how would we know that*/

/* RFC Specs on Closing:

CLOSED STATE (i.e., TCB does not exist)

If the user does not have access to such a connection, return "error: connection illegal for this process".
Otherwise, return "error: connection does not exist".

SYN-SENT STATE: Delete the TCB and return "error: closing" responses to any queued SENDs, or RECEIVEs.

SYN-RECEIVED STATE: If no SENDs have been issued and there is no pending data to send, 
then form a FIN segment and send it, and enter FIN-WAIT-1 state; otherwise, queue for processing 
after entering ESTABLISHED state.
*/

func (conn *VTCPConn) VClose() error {
	entry := conn.socketEntry

	/* already in closing process */
	if entry.state == FIN_WAIT_1 || entry.state == FIN_WAIT_2 || entry.state == CLOSED || entry.state == LAST_ACK || entry.state == TIME_WAIT {
		return errors.New("Connection already closing")
	}

	/* this is where we actually send the FIN */
	if entry.state == ESTABLISHED || entry.state == CLOSE_WAIT {
		/* wait until send buf is empty before sending FIN */
		fmt.Println("[VCLOSE] waiting for sendbuf to empty before sending FIN")
		for {
			conn.sendBuf.mu.Lock()
			noneInFlight := conn.sendBuf.nxt <= conn.sendBuf.una
			noneUnsent := conn.sendBuf.lbw < conn.sendBuf.nxt
			conn.sendBuf.mu.Unlock()
			
			if noneInFlight && noneUnsent {
				break
			}
			/* wait a tiny bit before checking again */
			time.Sleep(10 * time.Millisecond)
		}
		entry.sendFin()
	} else {
		fmt.Println("Strange case ocurred--VClose somehow called during handshake")
		return errors.New("Attempted closing during handshake")
	}

	return nil
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

