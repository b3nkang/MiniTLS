# Sending H1 → H3:

* `SendIP(10.2.0.3)`  
  * `src = 10.0.0.1`  
  * `dst = 10.2.0.3`  
  * `TTL = 16`  
* Check `H1` forwarding table  
  * Not local, use default  
  * Look up neighbour `10.0.0.2`, maps to `127.0.0.1:5001`  
  * Send packet `H1 if0` via UDP “link layer”  
* Packet received by `R1 if0`  
  * `R1` uses `IPstack` to parse packet, update TTL, run checksum, is dest \== my interface IPs? No? Forward  
* Check `R1` forwarding table  
  * Finds `10.2.0.0/24` in table which comes as an entry via RIP  
  * That address maps to next hop `10.1.0.2`, maps to `127.0.0.1:5003`  
  * Send via `R1 if1`  
* Packet received by `R2 if0`  
  * `R2` uses `IPstack` to parse packet, update TTL, run checksum, is `dest` \== my local interface IP? No? Does `dest` \== directly-connected prefix? Yes, `if1`? Forward to `if1`  
  * Look up neighbour `10.2.0.3`, maps to `127.0.0.1:5006`  
* Packet received by `H3 if0`  
  * `H3` uses `IPstack` to parse packet, update TTL, run checksum, is dest \== my interface IPs? Yes?  
    * Call protocol handler

# H1

Interface `if0`

* `Name: if0`  
* `Virtual IP: 10.0.0.1/24`  
* `UDP Addr: 127.0.0.1:5000`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.0.0.2`  
    * `UDP Addr: 127.0.0.1:5001`

  `]`

Forwarding Table

* `Prefix : Interface/Next hop`  
  * `10.0.0.0/24 : if0 (directly connected)`  
  * `0.0.0.0/0 : 10.0.0.2 (next hop via if0)`

# R1

Interface `if0`

* `Name: if0`  
* `Virtual IP: 10.0.0.2/24`  
* `UDP Addr: 127.0.0.1:5001`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.0.0.1 (H1)`  
    * `UDP Addr: 127.0.0.1:5000`

  `]`

Interface `if1`

* `Name: if1`  
* `Virtual IP: 10.1.0.1/24`  
* `UDP Addr: 127.0.0.1:5002`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.1.0.2 (R2)`  
    * `UDP Addr: 127.0.0.1:5003`

  `]`

Forwarding Table

* `Prefix : Interface/Next hop`  
  * `10.0.0.0/24 : if0 (directly connected)`  
  * `10.1.0.0/24 : if1 (directly connected)`  
  * `Routing rip`  
    * `10.2.0.0/24 : 10.1.0.2 (next hop via if1)`  
      * I.e. R2 periodically broadcasts its reachable prefix IPs to its RIP neighbour, which in this case is R1 per LNX. R1 receives msg “R2 can reach 10.2.0.0/24 with cost 0” and therefore incs cost and adds to fwdTbl

# R2

Interface `if0`

* `Name: if0`  
* `Virtual IP: 10.1.0.2/24`  
* `UDP Addr: 127.0.0.1:5003`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.1.0.1 (R1)`  
    * `UDP Addr: 127.0.0.1:5002`

  `]`


Interface `if1`

* `Name: if1`  
* `Virtual IP: 10.2.0.1/24`  
* `UDP Addr: 127.0.0.1:5004`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.2 (H2)`  
    * `UDP Addr: 127.0.0.1:5005`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.3 (H3)`  
    * `UDP Addr: 127.0.0.1:5006`

  `]`

Forwarding Table

* `Prefix : Interface/Next hop`  
  * `10.1.0.0/24 : if0 (directly connected)`  
  * `10.2.0.0/24 : if1 (directly connected)`  
  * `Routing rip`  
    * `10.0.0.0/24 : 10.1.0.1 (next hop via if0)`  
      * I.e. R1 periodically broadcasts its reachable prefix IPs to its RIP neighbour, which in this case is R2 per LNX. R2 receives msg “R1 can reach 10.0.0.0/24 with cost 0” and therefore incs cost and adds to fwdTbl

# H2

Interface `if0`

* `Name: if0`  
* `Virtual IP: 10.2.0.2/24`  
* `UDP Addr: 127.0.0.1:5005`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.1`  
    * `UDP Addr: 127.0.0.1:5004`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.3`  
    * `UDP Addr: 127.0.0.1:5006`

  `]`

Forwarding Table

* `Prefix : Interface/Next hop`  
  * `10.2.0.0/24 : if0 (directly connected)`  
  * `0.0.0.0/0 : 10.2.0.1 (next hop via if0)`

# H3

Interface `if0`

* `Name: if0`  
* `Virtual IP: 10.2.0.3/24`  
* `UDP Addr: 127.0.0.1:5006`  
* `Neighbours: [`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.1`  
    * `UDP Addr: 127.0.0.1:5004`  
  * `Neighbour:`  
    * `Virtual IP: 10.2.0.2`  
    * `UDP Addr: 127.0.0.1:5005`

  `]`

Forwarding Table

* `Prefix : Interface/Next hop`  
  * `10.2.0.0/24 : if0 (directly connected)`  
  * `0.0.0.0/0 : 10.2.0.1 (next hop via if0)`