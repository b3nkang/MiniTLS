package ipStack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"ip-isabelle-and-ben/pkg/lnxconfig"
	"log"
	"net"
	"net/netip"
	"time"
)
const (
    RIPRequest  = 1
    RIPResponse = 2
	Infinity = 16
)

/* Init RIP Neighbor Information and Routes */
func (stack *IPStack) InitRIP(config *lnxconfig.IPConfig) error {
	info := RipInfo{
		Neighbors: make([]RipNeighbour, 0),
		RipTimeout: config.RipTimeoutThreshold,
		RipUpdateRate: config.RipPeriodicUpdateRate,
	}
	/* pre-figure out interface name for each RIP neighbor to save time
		in the future */
	for _, ripIP := range config.RipNeighbors {
		found := false
		for _, iface := range stack.Interfaces {
			if _, exists := iface.Neighbours[ripIP]; exists {
				info.Neighbors = append(info.Neighbors, RipNeighbour{
					RouterIP: ripIP,
					InterfaceName: iface.Name,
				})
				found = true
				break
			}
		}
		if !found {
			log.Printf("RIP neighbor not found on any interface: %v\n", ripIP)
			return errors.New("invalid RIP neighbor")
		}
	}
	stack.RipInfo = info
	return nil
}

/* what main function will call to run goroutine */
func (stack *IPStack) UpdateLoop() {
	/* if we wait for first tick, update won't be sent for 5 seconds so must sent at t=0 */
	stack.SendPeriodicUpdate()
	
	/* now start ticker loop */
	ticker := time.NewTicker(stack.RipInfo.RipUpdateRate)
	defer ticker.Stop()

	for {
		select {
		case <- ticker.C:
			stack.SendPeriodicUpdate()
		}
	}
}

/* should be run at every timer tick -- sends periodic updates to neighbors */
/* TODO: ADD SCANNING OF ROUTES LAST UPDATED FIELD */
func (stack *IPStack) SendPeriodicUpdate() {
	/* protect Fwdtable */
	stack.mu.Lock()
	defer stack.mu.Unlock()

	for _, neighbor := range stack.RipInfo.Neighbors {
		/* list of RIPEntries that will be sent to this neighbor */
		routesToSend := make([]RipEntry, 0)

		/* prefix: entry */
		for _, entry := range stack.ForwardingTable {
			cost := entry.Cost
			/* Check SHPR conditions */
			if entry.Type == SourceTypeRIP { /* i.e. we learned about this route from another router ??*/
				if entry.NextHop == neighbor.RouterIP { /* the next hop for the route is this router */
					cost = Infinity
				}
			}
			routesToSend = append(routesToSend, RipEntry{
				Prefix: entry.Prefix,
				Cost: uint32(cost),
			})
		}
		RipMessage := RipMessage{
			Command: RIPResponse,
			NumEntries: uint16(len(routesToSend)),
			Entries: routesToSend,
		}
		stack.SendRipMessage(RipMessage, neighbor)
	}
}

/* converts RIP message into bytes, appends header, and sends to neighbor specified */
func (stack *IPStack) SendRipMessage(message RipMessage, neighbour RipNeighbour) error {
	/* Marshal and send RIP Message to this neighbor */
	bytes, err := message.Marshal()
	if err != nil {
		/* TODO: should we just continue here? answer: idk tbd -Ben */
		fmt.Println("Error converting RIP Message into bytes.")
		return err
	}
	
	/* source IP should be the IP of the interface that we send messages to this neighbor on */
	sourceInterface := stack.Interfaces[neighbour.InterfaceName]
	sourceIP := sourceInterface.IP
	destIP := neighbour.RouterIP
	/* Actual UDP Address: interface to send on -> neighbors -> RIP neighbor we're sending to -> their UDP address -> convert to net type*/
	udpAddr := net.UDPAddrFromAddrPort(sourceInterface.Neighbours[destIP].UDPAddr)

	/* create an IP packet to send */
	packet := SerializePacket(sourceIP, destIP, bytes, ProtocolTypeRIP, TTLNew, true)
	sourceInterface.LinkLayerSend(udpAddr, packet)
	return nil
}


/**** MAY WANT TO MOVE ELSEWHERE I JUST DON'T KNOW WHERE ****/

type RipMessage struct {
	Command uint16				/* Request/Response */
	NumEntries uint16			/* num routes being sent */
	Entries []RipEntry			/* actual entries */
}
type RipEntry struct {
	Cost 		uint32			/* cost of this router to get to address */
	Prefix 		netip.Prefix	/* contains both prefix and NETWORK address (specific VIP not needed) */
}

/* marshal rip message into bytes */
func (m *RipMessage) Marshal() ([]byte, error) {
    buf := new(bytes.Buffer)
    /* write command */
    if err := binary.Write(buf, binary.BigEndian, m.Command); err != nil {
        return nil, err
    }
	/* find and write numEntries */
    numEntries := uint16(len(m.Entries))
    if err := binary.Write(buf, binary.BigEndian, numEntries); err != nil {
        return nil, err
    }
	/* write each entry and do type conversions as necessary */
    for _, entry := range m.Entries {
		/* get addr as uint32 and mask as uint32 */
        addr := entry.Prefix.Addr().As4()
        mask := net.CIDRMask(entry.Prefix.Bits(), 32)
        addressUint := binary.BigEndian.Uint32(addr[:])
        maskUint := binary.BigEndian.Uint32(mask)

		/* write each entry field */
        if err := binary.Write(buf, binary.BigEndian, entry.Cost); err != nil {
            return nil, err
        }
        if err := binary.Write(buf, binary.BigEndian, addressUint); err != nil {
            return nil, err
        }
        if err := binary.Write(buf, binary.BigEndian, maskUint); err != nil {
            return nil, err
        }
    }
    return buf.Bytes(), nil
}

/* deserialize rip packet 
assume IP header is NOT included here */
func DeserializeRipMessage (data []byte) (*RipMessage, error) {
    buf := bytes.NewReader(data)
    var msg RipMessage
    var numEntries uint16

	/* read command and numEntries */
    if err := binary.Read(buf, binary.BigEndian, &msg.Command); err != nil {
        return nil, err
    }
    if err := binary.Read(buf, binary.BigEndian, &numEntries); err != nil {
        return nil, err
    }

	/* create entries structs */
    msg.Entries = make([]RipEntry, numEntries)
    for i := 0; i < int(numEntries); i++ {

        var cost, addrUint, maskUint uint32
		/* read each field as uint32 */
        if err := binary.Read(buf, binary.BigEndian, &cost); err != nil {
            return nil, err
        }
        if err := binary.Read(buf, binary.BigEndian, &addrUint); err != nil {
            return nil, err
        }
        if err := binary.Read(buf, binary.BigEndian, &maskUint); err != nil {
            return nil, err
        }
		
		/* supposedly convert back to netIP.Prefix. tbh this looks so sus but idk how
			to do it myself */
        addrBytes := make([]byte, 4)
        binary.BigEndian.PutUint32(addrBytes, addrUint)

        maskBytes := make([]byte, 4)
        binary.BigEndian.PutUint32(maskBytes, maskUint)
        prefixLen, _ := net.IPMask(maskBytes).Size()

		/* create new netip.Prefix struct from network address and mask */
        prefix := netip.PrefixFrom(
            netip.AddrFrom4([4]byte{
                addrBytes[0],
                addrBytes[1],
                addrBytes[2],
                addrBytes[3],
            }),
            prefixLen,
        )
        msg.Entries[i] = RipEntry{
            Prefix: prefix,
            Cost:   cost,
        }
    }
    return &msg, nil
}	


/*
RIP Format (from handout)
uint16 command
uint16 num_entries
struct {
    uint32 cost     // Integer value of cost
    uint32 address  // Byte representation of the IP address
    uint32 mask     // Byte representation of the subnet mask 
} entries[num_entries] 
*/