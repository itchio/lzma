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
	rrange uint32
	code   uint32
}

func makeReader(r io.Reader) Reader {
	if rr, ok := r.(Reader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

func newRangeDecoder(r io.Reader) (rd *rangeDecoder, err os.Error) {
	rd = &rangeDecoder{r: makeReader(r)}
	rd.rrange = 1<<32 - 1
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
	rd.code = uint32(buf[1]<<24) | uint32(buf[2]<<16) | uint32(buf[3]<<8) | uint32(buf[4])
	return
}

func (rd *rangeDecoder) decodeDirectBits(numTotalBits int) (res uint32, err os.Error) {
	for i := numTotalBits; i != 0; i-- {
		rd.rrange = rd.rrange >> 1
		t := uint32(((rd.code - rd.rrange) >> 1))
		rd.code -= rd.rrange & uint32(t-1)
		res = (res << 1) | (1 - t)
		if (rd.rrange & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange = rd.rrange << 8
		}
	}
	return
}

func (rd *rangeDecoder) decodeBit(probs []uint16, index int) (res uint32, err os.Error) {
	prob := probs[index]
	newBound := uint32((rd.rrange >> kNumBitModelTotalBits) * uint32(prob))
	if (rd.code ^ 0x80000000) < (newBound ^ 0x80000000) {
		rd.rrange = newBound
		probs[index] = prob + uint16((kBitModelTotal-uint32(prob))>>kNumMoveBits)
		if (rd.rrange & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 0
	} else {
		rd.rrange -= newBound
		rd.code -= newBound
		probs[index] = prob - uint16(uint32(prob)>>kNumMoveBits)
		if (rd.rrange & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 1
	}
	return
}

func initBitModels(length int) (probs []uint16) {
	probs = make([]uint16, length)
	val := uint16(kBitModelTotal >> 1)
	for i := 0; i < length; i++ {
		probs[i] = val
	}
	return
}
