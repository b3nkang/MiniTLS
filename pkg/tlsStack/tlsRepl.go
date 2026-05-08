package tlsStack

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
)

// usage:
//
//	tlsa <port>
//	tlsc <ip> <port>
func (tls *VTLSStack) HandleTlsReplCommands() {
	for cmd := range tls.TlsReplChan {
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}

		clientConfig, serverConfig := LoadTestTlsConfigs()

		switch parts[0] {
		case "tlsa":
			tls.handleTlsA(parts, serverConfig)
		case "tlsc":
			tls.handleTlsC(parts, clientConfig)
		case "tlss":
			tls.handleTlsS(parts)
		case "tlsr":
			tls.handleTlsR(parts)
		case "tlsls":
			tls.handleTlsLS()
		case "tlscl":
			tls.handleTlsCl(parts)
		default:
			fmt.Println("Unknown TLS command")
		}
	}
}

// tlsa <port>
//
// This is the TLS equivalent of TCP's "a" command.
// It does NOT require running TCP "a" first.
//
// Internally:
//   - VTLSListen calls tcpStack.VListen
//   - VTlsAccept calls tcpListener.VAccept
//   - VTlsAccept then runs HandleServerKeyExchange
func (tls *VTLSStack) handleTlsA(parts []string, serverConfig VTLSServerConfig) {
	if len(parts) != 2 {
		fmt.Println("Usage: tlsa <port>")
		return
	}

	portInt, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Port must be an integer")
		return
	}
	if portInt < 0 || portInt > 65535 {
		fmt.Println("Port must be a number 0-65535")
		return
	}

	port := uint16(portInt)

	go func() {
		listener, err := tls.VTLSListen(port, serverConfig)
		if err != nil {
			fmt.Println("[tlsa] listen error:", err)
			return
		}

		fmt.Printf("[tlsa] TLS listening on port %d\n", port)

		for {
			conn, err := listener.VTLSAccept()
			if err != nil {
				fmt.Println("[tlsa] accept/handshake error:", err)
				return
			}

			id := tls.registerConn(conn)
			conn.PrintHandshakeDebug("server")
			fmt.Printf("[tlsa] New TLS connection with ID %d\n", id)
		}
	}()
}

// tlsc <vip> <port>
//
// This is the TLS equivalent of TCP's "c" command.
// It does NOT require running TCP "c" first.
//
// Internally:
//   - VTLSDial calls tcpStack.VConnect
//   - VTLSDial then runs HandleUserKeyExchange
func (tls *VTLSStack) handleTlsC(parts []string, clientConfig VTLSClientConfig) {
	if len(parts) != 3 {
		fmt.Println("Usage: tlsc <vip> <port>")
		return
	}

	addr, err := netip.ParseAddr(parts[1])
	if err != nil {
		fmt.Println("Invalid IP Format:", parts[1])
		return
	}

	portInt, err := strconv.Atoi(parts[2])
	if err != nil {
		fmt.Println("Port must be an integer")
		return
	}
	if portInt < 0 || portInt > 65535 {
		fmt.Println("Port must be a number 0-65535")
		return
	}

	port := uint16(portInt)

	go func() {
		conn, err := tls.VTLSDial(addr, port, clientConfig)
		if err != nil {
			fmt.Println("[tlsc] dial/handshake error:", err)
			return
		}

		id := tls.registerConn(conn)
		conn.PrintHandshakeDebug("client")
		fmt.Printf("[tlsc] New TLS connection with ID %d\n", id)
	}()
}

/* send bytes via tls (put in ID + bytes to send) */
func (tls *VTLSStack) handleTlsS(parts []string) {
	if len(parts) != 3 {
        fmt.Println("Usage: tlss <connID> <message>")
        return
    }

    id, err := strconv.Atoi(parts[1])
    if err != nil {
        fmt.Println("connID must be an integer")
        return
    }

    conn := tls.getConn(id)
    if conn == nil {
        fmt.Printf("[tlss] no TLS connection with ID %d\n", id)
        return
    }

    go func() {
		/* calls WriteFull() -> VWrite -> can block, so run in goroutine */
        n, err := conn.VTLSWrite([]byte(parts[2]))
        if err != nil {
            fmt.Println("[tlss] write error:", err)
            return
        }
        fmt.Printf("[tlss] sent %d bytes\n", n)
    }()
}

/* read bytes from tls conn */
func (tls *VTLSStack) handleTlsR(parts []string) {
	if len(parts) != 3 {
        fmt.Println("Usage: tlsr <connID> <numBytes>")
        return
    }

    id, err := strconv.Atoi(parts[1])
    if err != nil {
        fmt.Println("connID must be an integer")
        return
    }

    numBytes, err := strconv.Atoi(parts[2])
    if err != nil {
        fmt.Println("numBytes must be an integer")
        return
    }

    conn := tls.getConn(id)
    if conn == nil {
        fmt.Printf("[tlsr] no TLS connection with ID %d\n", id)
        return
    }

    go func() {
        buf := make([]byte, numBytes)
        n, err := conn.VTLSRead(buf)
        if err != nil {
            fmt.Println("[tlsr] read error:", err)
            return
        }
        fmt.Printf("[tlsr] read %d bytes: %s\n", n, string(buf[:n]))
    }()
}

/* close TLS connection */
func (tls *VTLSStack) handleTlsCl(parts []string) {
	if len(parts) != 2 {
        fmt.Println("Usage: tlscl <connID>")
        return	
	}
    id, err := strconv.Atoi(parts[1])
    if err != nil {
        fmt.Println("connID must be an integer")
        return
    }

    conn := tls.getConn(id)
    if conn == nil {
        fmt.Printf("[tlscl] no TLS connection with ID %d\n", id)
        return
    }
	/* call VClose() (tcp) */
    if err := conn.VTLSClose(); err != nil {
        fmt.Printf("[tlscl] close error: %v\n", err)
        return
    }

	/* remove conn from tls table */
	tls.connMu.Lock()
    delete(tls.tlsConnMap, id)
    tls.connMu.Unlock()

	fmt.Printf("[tlscl] closed TLS connection %d\n", id)
}

/* list tls sockets using TLS socket table and TCP socket table info */
func (tls *VTLSStack) handleTlsLS() {

	tw := tablewriter.NewWriter(os.Stdout)

	tw.Header([]string{
		"TLS ID",
		"LAddr",
		"LPort",
		"RAddr",
		"RPort",
		"Status",
	})
	table := tls.tlsConnMap
	tls.connMu.Lock()
	defer tls.connMu.Unlock()

	for id, conn := range table {

		connInfo := conn.tcpConn.GetInfo()

		laddr := addrToString(connInfo.LocalIP)
		raddr := addrToString(connInfo.RemoteIP)

		lport := fmt.Sprint(connInfo.LocalPort)
		rport := fmt.Sprint(connInfo.RemotePort)

		stateStr := stateToString(connInfo.State)

		tw.Append([]string{
			fmt.Sprint(id),
			laddr,
			lport,
			raddr,
			rport,
			stateStr,
		})
	}
	tw.Render()
}



/* add vtlsconn to conn map */
func (tls *VTLSStack) registerConn(conn *VTLSConn) int {
    tls.connMu.Lock()
    defer tls.connMu.Unlock()
    id := tls.nextID
    tls.tlsConnMap[id] = conn
    tls.nextID++
    return id
}

/* super basic helper to get conn by ID from map */
func (tls *VTLSStack) getConn(id int) *VTLSConn {
    tls.connMu.Lock()
    defer tls.connMu.Unlock()
    return tls.tlsConnMap[id]
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
	case FIN_WAIT_1:
		return "FIN_WAIT_1"
	case CLOSE_WAIT:
		return "CLOSE_WAIT"
	case LAST_ACK:
		return "LAST_ACK"
	case FIN_WAIT_2:
		return "FIN_WAIT_2"
	case TIME_WAIT:
		return "TIME_WAIT"
	case CLOSED:
		return "CLOSED"
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
