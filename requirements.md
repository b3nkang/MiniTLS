# What each obj needs to do

`Interface:`  
- `Needs to listen for packets —> thread, pass info to ipObj`  
- `Needs to send packets to neighbours —> send to some other UDP addr in subnet`  
- `Check if dest is in interface IP or neighbours`  
	  
`IPStack:`  
- `Needs to set up everything	—> build obj from config lnx`  
- `Needs to handle incoming packet data, do computation for:`  
    - `Parse`  
	- `Checksum`  
	- `TTL, if 0, drop`  
- `Prefix match:`  
	- `If directly connected:`  
		- `Send to relevant interface`  
	- `Else:`  
		- `Send to next hop (?)`	  
- `Needs to handle updating ForwardingTable for RIP changes (???)`  
- `Needs to overall send the msg —> call whatever lower level functions ???`   
	- `This is SendIP()`