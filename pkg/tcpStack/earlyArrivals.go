package tcpstack

import (
	"container/heap"
)

/* utils for adding our own segments */
func (h *EarlyArrivals) PushSegment(startSeq uint32, data []byte) {
	if len(data) == 0{
		return
	}

	/* construct EarlyArrival obj to push */
	cp := make([]byte, len(data))
	copy(cp, data)

	heap.Push(h, &EarlyArrival{
		startSeq: startSeq,
		endSeq:   startSeq + uint32(len(cp)),
		data:     cp,
	})

}

/* initialize heap */
func MakeEarlyArrivals() *EarlyArrivals {
	h := make(EarlyArrivals, 0)
	heap.Init(&h)
	return &h
}

/* retrieve smallest seqNum Early Arrival */
func (h *EarlyArrivals) PopMin() *EarlyArrival {
	if h.Len() == 0 {
		return nil
	}
	return heap.Pop(h).(*EarlyArrival)
}

/* get total length of DATA in the queue */
func (h EarlyArrivals) TotalDataLen() uint32 {
    var total uint32
    for _, seg := range h {
        total += uint32(len(seg.data))
    }
    return total
}



/* -------required methods to implement for Go heap Interface (used by App fxns)------- */

func (h EarlyArrivals) Len() int { return len(h) }

/* rule for organizing heap: True when i's seq num 
	is LESS than j -> i goes first */
func (h EarlyArrivals) Less(i, j int) bool {
	return h[i].startSeq < h[j].startSeq
}

func (h EarlyArrivals) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *EarlyArrivals) Push(x any) {
	*h = append(*h, x.(*EarlyArrival))
}

func (h *EarlyArrivals) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func (h EarlyArrivals) Peek() *EarlyArrival {
	if len(h) == 0 {
		return nil
	}
	/* return first item in heap without removing it */
	return h[0]
}