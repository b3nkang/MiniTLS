package linkLayer

import (
	"fmt"
	"log"
	"net"
)

const MaxMessageSize = 1400

/* send an IP packet (IPV4 Header + bytes message) via UDP Link Layer */
func (iface *Interface) LinkLayerSend(udpDest *net.UDPAddr, bytes []byte) {
	if !iface.Up {
		fmt.Printf("Dropping packet before sending on down interface: %s", iface.Name)
		return
	}
	_, err := iface.Conn.WriteToUDP(bytes, udpDest)
	if err != nil {
		log.Panicln("Error writing to socket: ", err)
	}
}


/* what to run to constantly listen for new messages on your UDP port */
func (iface *Interface) LinkLayerListen(ipStackChan chan []byte) error {
	for {
		buffer := make([]byte, MaxMessageSize)

		/* Read messages from UDP port */
		_, _, err := iface.Conn.ReadFromUDP(buffer)
		if err != nil {
			log.Panicln("Error reading from UDP socket ", err)
		}
		
		// fmt.Printf("[LL] Received IP packet. Forwarding to IP Stack...\n")
		if !iface.Up {
			fmt.Printf("Dropping packet before receiving on down interface: %s", iface.Name)
			continue
		}

		ipStackChan <- buffer
	}
}	