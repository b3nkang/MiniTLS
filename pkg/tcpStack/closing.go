package tcpstack

/*

Cycle from notes:

ACTIVE CLOSE (side that chooses to close first):
	- Send FIN: segment with sequence num X+1 (takes up a sequence num) and FIN flag
		- switch to FIN_WAIT_1 state
	- Receive ACK for FIN: ACK with X+2 means they got it
		- switch to FIN_WAIT_2 state
		- means we can no longer send data, but we can receive data
		- keep receiving data here
	- Receive FIN from other side (takes up a sequence num for them too), this means other side is done sending
		- switch to TIME_WAIT state
		- wait for a set period of time until we delete socket table entry
	- time runs out: CLOSED
		- delete socket table entry and do other cleanup as necessary

PASSIVE CLOSE (side that is closed on):
	- RECEIVE FIN
		- send ACK that we got that FIN (ACKnum is that seqNum + 1)
		- switch to state CLOSE_WAIT
	- Keep sending data if we want until VClose() is called

*/

