package tlsStack

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
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

			conn.PrintHandshakeDebug("server")
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

		conn.PrintHandshakeDebug("client")
	}()
}