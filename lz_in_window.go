package lzma

import (
	"io"
	"os"
)

type lzInWindow struct {
	r              io.Reader
	buf            []byte
	posLimit       int32
	lastSafePos    int32
	bufOffset      int32
	blockSize      int32
	pos            int32
	keepSizeBefore int32
	keepSizeAfter  int32
	streamPos      int32
	streamEnd      bool
}

func newLzInWindow(r io.Reader, keepSizeBefore, keepSizeAfter, keepSizeReserv int32) (iw *lzInWindow, err os.Error) {
	blockSize := keepSizeBefore + keepSizeAfter + keepSizeReserv
	iw = &lzInWindow{
		r:              r,
		buf:            make([]byte, blockSize),
		lastSafePos:    blockSize - keepSizeAfter,
		bufOffset:      0,
		blockSize:      blockSize,
		pos:            0,
		keepSizeBefore: keepSizeBefore,
		keepSizeAfter:  keepSizeAfter,
		streamPos:      0,
		streamEnd:      false,
		// posLimit: initialized in readBlock()
	}
	err = iw.readBlock()
	return
}

func (iw *lzInWindow) moveBlock() {
	offset := iw.bufOffset + iw.pos - iw.keepSizeBefore
	if offset > 0 {
		offset--
	}
	numBytes := iw.bufOffset + iw.streamPos - offset
	for i := int32(0); i < numBytes; i++ {
		iw.buf[i] = iw.buf[offset+i]
	}
	iw.bufOffset -= offset
}

func (iw *lzInWindow) readBlock() (err os.Error) {
	if iw.streamEnd {
		return
	}
	for {
		if iw.blockSize-iw.bufOffset-iw.streamPos == 0 {
			return
		}
		n, err := iw.r.Read(iw.buf[iw.bufOffset+iw.streamPos : iw.blockSize])
		if err != nil && err != os.EOF {
			return
		}
		if n == 0 && err == os.EOF {
			iw.posLimit = iw.streamPos
			ptr := iw.bufOffset - iw.posLimit
			if ptr > iw.lastSafePos {
				iw.posLimit = iw.lastSafePos - iw.bufOffset
			}
			iw.streamEnd = true
			return
		}
		iw.streamPos += int32(n)
		if iw.streamPos >= (iw.pos + iw.keepSizeAfter) {
			iw.posLimit = iw.streamPos - iw.keepSizeAfter
		}
	}
	return
}

func (iw *lzInWindow) movePos() (err os.Error) {
	iw.pos++
	if iw.pos > iw.posLimit {
		ptr := iw.bufOffset + iw.pos
		if ptr > iw.lastSafePos {
			iw.moveBlock()
		}
		err = iw.readBlock()
		if err != nil {
			return
		}
	}
	return
}

func (iw *lzInWindow) indexByte(index int32) byte {
	return iw.buf[iw.bufOffset+iw.pos+index]
}

func (iw *lzInWindow) matchLen(index, distance, limit int32) (res int32) {
	if iw.streamEnd == true {
		if iw.pos+index+limit > iw.streamPos {
			limit = iw.streamPos - iw.pos - index
		}
	}
	distance++
	pby := iw.bufOffset + iw.pos + index
	for res = int32(0); res < limit && iw.buf[pby+res] == iw.buf[pby+res-distance]; res++ {
	}
	return
}

func (iw *lzInWindow) availableBytes() int32 {
	return iw.streamPos - iw.pos
}

func (iw *lzInWindow) reduceOffsets(subValue int32) {
	iw.bufOffset += subValue
	iw.posLimit -= subValue
	iw.pos -= subValue
	iw.streamPos -= subValue
}
