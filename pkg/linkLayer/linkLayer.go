package linkLayer

import (
	"fmt"
	"log"
	"net"
)

const MaxMessageSize = 1400

/* send an IP packet (IPV4 Header + bytes message) via UDP Link Layer */
func (iface *Interface) LinkLayerSend(udpDest *net.UDPAddr, bytes []byte) {
	bytesWritten, err := iface.Conn.WriteToUDP(bytes, udpDest)
	if err != nil {
		log.Panicln("Error writing to socket: ", err)
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
}


/* what to run to constantly listen for new messages on your UDP port */
func (iface *Interface) LinkLayerListen(ipStackChan chan IPPacket) error {
	for {
		buffer := make([]byte, MaxMessageSize)

		/* Read messages from UDP port */
		_, sourceAddr, err := iface.Conn.ReadFromUDP(buffer)
		if err != nil {
			log.Panicln("Error reading from UDP socket ", err)
		}
		
		fmt.Printf("[LL] Received IP packet from %s. Forwarding to IP Stack...\n", sourceAddr.String())

		packet := IPPacket{
			SrcIfaceAddr: sourceAddr,
			Data: buffer,
		}

		ipStackChan <- packet
	}
}