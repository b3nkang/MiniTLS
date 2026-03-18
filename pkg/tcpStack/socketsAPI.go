package tcpstack

import "ip-isabelle-and-ben/pkg/ipStack"

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
	}

	/* increment next ID for next entry */
	tcp.socketTable.nextID++
	/* add this entry to table */
	tcp.socketTable.socketMap[tableEntry.socketID] = tableEntry

	return listener, nil
}

/* listen for new connections until someone connects */
func (listener *VTCPListener) VAccept() (*VTCPConn, error) {
	listener.acceptingConns = true
	conn := <-listener.connChan
	return conn, nil
}
