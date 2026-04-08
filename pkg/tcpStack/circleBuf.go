package tcpstack

func NewCircleBuf(maxSize uint32, baseSeq uint32) *CircleBuf {
    return &CircleBuf {
        buf: make([]byte, MAX_WIN_SIZE),
        currSize: 0,
        maxSize: MAX_WIN_SIZE,
        baseSeq: baseSeq,
        head: 0,
    }
}


func (cb *CircleBuf) SeqNumToIndex(seqNum uint32) int {
	offset := int(seqNum - cb.baseSeq)
	return (cb.head + offset) % int(cb.maxSize)
}

func (cb *CircleBuf) FreeSpace() uint32 {
    return cb.maxSize - cb.currSize
}

func (cb *CircleBuf) IsEmpty() bool {
    return cb.currSize == 0
}

func (cb *CircleBuf) WriteIntoBuf(seqNum uint32, data []byte) int {
    if len(data) == 0 {
        return 0
    }

    start := cb.SeqNumToIndex(seqNum)
    firstChunk := min(len(data), len(cb.buf)-start) // write either the whole block of data or hit the end and wrap around

    copy(cb.buf[start:start+firstChunk], data[:firstChunk])

    remaining := len(data) - firstChunk
    if remaining > 0 { // wrap around with the rest
        copy(cb.buf[0:remaining], data[firstChunk:])
    }

    cb.currSize += uint32(len(data)) // TODO: may need to update this line after mstone2
    return len(data)
}

/* read n bytes into provided buffer starting at seq */
func (cb *CircleBuf) ReadFromBuf(seqNum uint32, dest []byte, n uint32) int {
    if n < 1 || len(dest) == 0 {
		return 0
	}
    /* truncate num bytes to read if necessary --won't be necessary */
	if n > uint32(len(dest)) {
		n = uint32(len(dest))
	}

    /* get index of starting seq */
    start := cb.SeqNumToIndex(seqNum)
    firstChunk := min(int(n), len(cb.buf)-start) // read either the whole block of data or hit the end and wrap around

	copy(dest[:firstChunk], cb.buf[start:start+firstChunk])

    remaining := int(n) - firstChunk
    if remaining > 0 { // wrap around with the rest
		copy(dest[firstChunk:firstChunk+remaining], cb.buf[0:remaining])
    }

    return int(n)
}

/* extract 'n' in-order bytes from circular buffer starting at sequence num 'seq' */
func (cb *CircleBuf) SliceFrom(seq uint32, n uint32) []byte {
	if n == 0 {
		return make([]byte, 0)
	}

	out := make([]byte, n)
	cb.ReadFromBuf(seq, out, n)
	return out
}

/* reclaim space in buffer after ACKing bytes */
func (cb *CircleBuf) AdvanceBase(numBytesAcked uint32) {
	if numBytesAcked == 0 {
		return
	}
    /* deal with edge case that ACK is greater than current size */
	if numBytesAcked > cb.currSize {
		numBytesAcked = cb.currSize
	}

    /* advance base sequence num, decrease buffer size, and move head */
	cb.baseSeq += numBytesAcked
	cb.currSize -= numBytesAcked
	cb.head = (cb.head + int(numBytesAcked)) % int(cb.maxSize)
}