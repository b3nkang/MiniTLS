### File Structure:

* Cmd:  
  * Vhost and vrouter programs  
* Pkg:  
  * ipStack package: contains all functionality related to IP forwarding and RIP  
    * [ipStack.go](http://ipStack.go): logic for IP forwarding, packet sending, and data structure initialization  
    * replAndPrint: methods run by REPL to respond to commands and print data structures  
    * [rip.go](http://rip.go): logic for RIP: all proactive loops, triggered messages, serialization stuff, RIP entry handling, routing table updating, etc  
    * [types.go](http://types.go): all data structures mentioned in the following section  
  * linkLayer package: contains methods and data structures for sending on the “link layer”  
* Lnxconfig:   
  * Lnx file parsing copied from sample code  
* Protocol:  
  * Miscellaneous utility for computing/validation checksums and handling test messages

### Data Structures:

Our IP Stack (see ipStack/[types.go](http://types.go)) contains the following data structures:

* **Interface map**: maps interface name (“if0”) to Interface object  
* **Forwarding table**: maps network prefixes to FwdTable entries  
  * Used by hosts and routers for IP forwarding  
  * Used by routers for RIP  
    * Contains fields like Cost and LastUpdated used only by routers  
* **Incoming packet channel:** tells main/processing goroutine that the listener process has received a packet at its port  
* **Mutex:** used by routers to protect their routing table  
* **Receive Handler Map:** maps protocol number (0, 200\) to the function used to “handle” messages that arrive via that protocol  
* **RIP Data structures:**  
  * **RIP information:** used only by routers to store config-provided RIP info (neighbors, timeout, and update rate)  
  * **RipMessage:** represents the message part of a RIP packet, containing command, entry list length, and entry list  
  * **RipEntry:** represents entry in RIP message for 1 route

We factored the following UDP-related data structures into a separate Link Layer package (linkLayer/types.go):

* **Interface:** contains own prefix and VIP, also its Conn object used for sending and receiving packets.  
  * Contains **Neighbor map**: maps VIP to neighbor object for directly accessible neighbors on the same local network  
* **Neighbor:** contains VIP and a UDP address \+ port number for “link layer” communication

### Vhost and Vrouter

Both the vhost and vrouter use our InitIPStackFromConfig method to create their IP stacks. Most of the rest of the code is in our IPStack, however the host and router call slightly different methods on their IPStacks to get themselves up and running.

* **Vhost:**  
  * Registers a receive handler for protocol 0 to print out test packets when they reach host  
  * Runs IP layer: when packets come in, hosts deserialize and validate them to see if they reached the right destination  
  * Runs its REPL  
* **Vrouter:**  
  * Registers a receive handler for protocol 200 to properly handle RIP messages when they are received  
  * Runs IP layer: when packets come in, routers deserialize and validate them before determining if they need to be forwarded and where  
  * Runs RIP loops:  
    * UpdateLoop: periodically updates neighbors of its own routing table  
    * TimeoutLoop: periodically checks routing table to see if entries have timed out  
  * Runts its REPL

### Goroutines

We use goroutines for:

* Each interface to listen for incoming packets  
* REPLs  
* Each node to handle any incoming packet (send from listener thread via channel)  
* RIP loops (mentioned above)

### IP Packet Processing

When a packet arrives at a node, it goes through the following steps, as orchestrated by [ipStack.go](http://ipStack.go)’s RunIPLayer() method:

1. Packet received in ipStack’s packet channel  
2. Packet is deserialized and checksum is validated, TTL validated  
3. Destination check: if destination reached, consult Handler function map and call proper handler linked to protocol type  
4. Forwarding: TTL decremented, longest prefix match performed  
   1. Either a local match, default, or next hop match is found, or packet is dropped  
   2. If the desired route’s destination is offline (cost \= Infinity), the packet is dropped  
5. Link Layer Sending:   
   1. Node finds the correct interface in their interfaces map and looks up dest in that interface’s neighbors table, from which we get its UDP address and send the packet

### RIP

We decided to include RIP functionality in the IPStack package even though it’s only relevant for routers because it is inherently tied to internal IPStack data structures, and we didn’t want to expose those to another package’s methods. 

**Routing Table**

* In addition to the information stored in the hosts’ forwarding tables, we store each route’s cost, last time updated, and type (RIP, local or static)   
* Our routing table is also protected with a mutex because we may have two goroutines using or updating the same routing table at the same time.