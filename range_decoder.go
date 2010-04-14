package lzma

import (
	"bufio"
	"io"
	"os"
)

const (
	kTopMask              uint32 = 0xff000000 // int32(-1 << 24)
	kNumBitModelTotalBits uint32 = 11
	kBitModelTotal        uint32 = 1 << 11
	kNumMoveBits          uint32 = 5
)

type Reader interface {
	io.Reader
	ReadByte() (c byte, err os.Error)
}

type rangeDecoder struct {
	r      Reader
	rrange int32
	code   int32
}

func makeReader(r io.Reader) Reader {
	if rr, ok := r.(Reader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

func newRangeDecoder(r io.Reader) (rd *rangeDecoder, err os.Error) {
	rd = &rangeDecoder{r: makeReader(r)}
	rd.rrange = -1
	rd.code = 0
	buf := make([]byte, 5)
	n, err := rd.r.Read(buf)
	if err != nil {
		return
	}
	if n != len(buf) {
		err = os.NewError("expected " + string(len(buf)) + " bytes, read " + string(n) + " bytes instead")
		return
	}
	rd.code = int32(uint32(buf[1]<<24) | uint32(buf[2]<<16) | uint32(buf[3]<<8) | uint32(buf[4]))
	return
}

func (rd *rangeDecoder) decodeDirectBits(numTotalBits uint32) (res uint32, err os.Error) {
	for i := numTotalBits; i != 0; i-- {
		rd.rrange = int32(uint32(rd.rrange) >> 1)
		t := int32(uint32(rd.code-rd.rrange) >> 1)
		rd.code -= rd.rrange & (t - 1)
		res = (res << 1) | uint32(1-t)
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
	}
	return
}

func (rd *rangeDecoder) decodeBit(probs []uint16, index uint32) (res uint32, err os.Error) {
	prob := int32(probs[index])
	newBound := int32(uint32(rd.rrange)>>kNumBitModelTotalBits) * prob
	if rd.code^int32(-1<<31) < newBound^int32(-1<<31) {
		rd.rrange = newBound
		probs[index] = uint16(uint32(prob) + uint32(kBitModelTotal-uint32(prob))>>kNumMoveBits)
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 0
	} else {
		rd.rrange -= newBound
		rd.code -= newBound
		probs[index] = uint16(uint32(prob) - uint32(prob)>>kNumMoveBits)
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 1
	}
	return
}

func initBitModels(length uint32) (probs []uint16) {
	probs = make([]uint16, int(length))
	val := uint16(kBitModelTotal >> 1)
	for i := uint32(0); i < length; i++ {
		probs[i] = val
	}
	return
}
