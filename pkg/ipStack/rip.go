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

	ipv4header "github.com/brown-csci1680/iptcp-headers"
)
const (
    RIPRequest  = 1
    RIPResponse = 2
	Infinity = 16
)

/* Init RIP routes, RIP Neighbor Information and Routes */
func (stack *IPStack) initRIP(config *lnxconfig.IPConfig) error {
	/* mark static routes as cost = 0 (not done for hosts) */
	for _, entry := range stack.ForwardingTable {
		if entry.Type == SourceTypeStatic {
			entry.Cost = 0
		}
	}

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

// highest-level function to spin up a checker for timed-out fwdEntries
func (ipStack *IPStack) TimeoutLoop() {
	ticker := time.NewTicker(ipStack.RipInfo.RipTimeout / 6) // 2 seconds
	defer ticker.Stop()

	for {
		select {
		case <- ticker.C:
			ipStack.CheckForTimeouts()
		}
	}
}

func (ipStack *IPStack) CheckForTimeouts() {
	/* protect forwarding table */
	ipStack.mu.Lock()
    defer ipStack.mu.Unlock()

    now := time.Now()
	/* for storing the messages we will send to RIP neighbors about down routers */
    var timedOutRipEntries []RipEntry
	for prefix, entry := range ipStack.ForwardingTable {
		/* must have recieved through rip to have a timeout field */
		if entry.Type != SourceTypeRIP {
			continue
		}
		/* if RipTimout maximum has been exceeded */
		if now.Sub(entry.LastUpdated) >= ipStack.RipInfo.RipTimeout {
			if entry.Cost >= Infinity {
				// already timed out, skip processing
				continue
			}
			fmt.Printf("FOUND TIMEOUT: %s\n", prefix.Addr().String())
            timedOutRipEntries = append(timedOutRipEntries, RipEntry{ Cost: 16, Prefix: prefix })
			entry.Cost = Infinity
		}
	}
    if len(timedOutRipEntries) > 0 {
        go ipStack.SendTriggeredUpdate(timedOutRipEntries)
    }
}


/* what main function will call to run goroutine */
func (stack *IPStack) UpdateLoop() {
	/* first, request all our neighbors to send us RIP messages */
	stack.sendRIPRequest()

	/* if we wait for first tick, update won't be sent for 5 seconds so must sent at t=0 */
	stack.sendPeriodicUpdate()
	
	/* now start ticker loop */
	ticker := time.NewTicker(stack.RipInfo.RipUpdateRate)
	defer ticker.Stop()

	for {
		select {
		case <- ticker.C:
			stack.sendPeriodicUpdate()
		}
	}
}

/* send every neighbor a RIP request */
func (stack *IPStack) sendRIPRequest() {
	request := RipMessage{
		Command: RIPRequest,
	}
	for _, neighbor := range stack.RipInfo.Neighbors {
		err := stack.SendRipMessage(request, neighbor)
		if err != nil {
			fmt.Printf("Error sending triggered update to Neighbor: $%s. Just gonna keep going tho\n", neighbor.RouterIP.String())
		}
	}
}

/* should be run at every timer tick -- sends periodic updates to neighbors */
func (stack *IPStack) sendPeriodicUpdate() {
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
			// fmt.Printf("ADDING TO RIP FWDING: to %s, add prefix %s\n\n", neighbor.RouterIP.String(), entry.Prefix.String())
			routesToSend = append(routesToSend, RipEntry{
				Prefix: entry.Prefix,
				Cost: uint32(cost),
			})
		}
		ripMessage := RipMessage{
			Command: RIPResponse,
			NumEntries: uint16(len(routesToSend)),
			Entries: routesToSend,
		}
		err := stack.SendRipMessage(ripMessage, neighbor)
		if err != nil {
			fmt.Printf("Error sending periodic update to Neighbor: $%s. Just gonna keep going tho\n", neighbor.RouterIP.String())
		}
	}
}

/* sends specific updates with changed information to RIP neighbors */
func (stack *IPStack) SendTriggeredUpdate(entries []RipEntry) {
	fmt.Println("Sending triggered update")
	message := RipMessage{
		Command: RIPResponse,
		NumEntries: uint16(len(entries)),
		Entries: entries,
	}
	for _, neighbor := range stack.RipInfo.Neighbors {
		err := stack.SendRipMessage(message, neighbor)
		if err != nil {
			fmt.Printf("Error sending triggered update to Neighbor: $%s. Just gonna keep going tho\n", neighbor.RouterIP.String())
		}
	}
}

/* converts RIP message into bytes, appends header, and sends to neighbor specified */
func (stack *IPStack) SendRipMessage(message RipMessage, neighbour RipNeighbour) error {
	/* Marshal and send RIP Message to this neighbor */
	bytes, err := message.Marshal()
	if err != nil {
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

// ******************** HANDLE INCOMING RIP MESSAGE LOGIC *******************
/* only called once we know RIP message has arrived at final dest */
func (ipStack *IPStack) HandleRipMessage(hdr *ipv4header.IPv4Header, messageBytes []byte) {

	ripMsg, err := DeserializeRipMessage(messageBytes)
	if err != nil {
		fmt.Printf("[IP] Received malformed RIP data, unable to deserialize. Error: %s\n> ", err)
		return
	}

	/* see if we got a request, no need to mutate table, just send update */
	if ripMsg.Command == RIPRequest {
		ipStack.sendPeriodicUpdate()
		return 
	}

	// lock while we update table
	ipStack.mu.Lock()
	defer ipStack.mu.Unlock()
	ipStack.ListRoutes() 

	/* for keeping track of changed entries */
	changedEntries := make([]RipEntry, 0)

	for _, ripEntry := range ripMsg.Entries {
		fmt.Printf("entry: %s can reach %s with cost=%d\n", hdr.Src.String(), ripEntry.Prefix.String(), int(ripEntry.Cost))
		currFwdEntry, exists := ipStack.ForwardingTable[ripEntry.Prefix]
		// update cost
		var ripEntryCost uint32
		if ripEntry.Cost < Infinity {
			ripEntryCost += ripEntry.Cost + 1
		} else {
			ripEntryCost = Infinity
		}
		// check if exists, if not, add
		if !exists {
			ipStack.UpdateForwardingTable(ripEntry, hdr, "", ripEntryCost)
			changedEntries = append(changedEntries, ripEntry)
			continue
		} 
		// if it already is in table, check if we can even update it
		if currFwdEntry.Type != SourceTypeRIP {
			// fmt.Printf("[RIP] Received RipEntry but FwdEntry was not of type RIP\n> ")
			continue
		}
		// entry exists:
		// if lower cost, just update
		currFwdEntryCost := currFwdEntry.Cost
		if ripEntryCost < currFwdEntryCost {
			ipStack.UpdateForwardingTable(ripEntry, hdr, "", ripEntryCost)
			changedEntries = append(changedEntries, ripEntry)
			continue
		}
		// else, if it doesn't equal
		if ripEntryCost > currFwdEntryCost {
			// If the route we currently use is THROUGH the neighbor who is talking to us,
			// we MUST take their new metric (even if worse / INF), and refresh LastUpdated.
			if hdr.Src == currFwdEntry.NextHop {
				ipStack.UpdateForwardingTable(ripEntry, hdr, "", ripEntryCost)
				changedEntries = append(changedEntries, ripEntry)
				continue
			} 
			// else, curr cost is better, ignore
		}
		// else if everything is the same
		if ripEntryCost == currFwdEntryCost && hdr.Src == currFwdEntry.NextHop {
			currFwdEntry.LastUpdated = time.Now()
		}
	}
	/* if our costs changed, trigger a new RIPMessage to be sent */
	if len(changedEntries) != 0 {
		ipStack.SendTriggeredUpdate(changedEntries)
	}
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
	/* if we got a request, just be done */
	if msg.Command == RIPRequest {
		return &msg, nil
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