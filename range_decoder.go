package lzma

import (
	"bufio"
	//"fmt"
	"io"
	"os"
)

const (
	kTopMask              uint32 = 0xff000000 // int32(-1 << 24)
	kNumBitModelTotalBits uint32 = 11
	kBitModelTotal        uint32 = 1 << kNumBitModelTotalBits
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
	//rd.code = int32(uint32(buf[1]<<24) | uint32(buf[2]<<16) | uint32(buf[3]<<8) | uint32(buf[4]))
	for i := 0; i < len(buf); i++ {
		rd.code = rd.code<<8 | int32(buf[i])
	}
	return
}

func (rd *rangeDecoder) decodeDirectBits(numTotalBits uint32) (res uint32, err os.Error) {
	//fmt.Printf("rd.decoder.decodeDirectBits() [0]: numTotalBits = %d, Range = %d, Code = %d\n",
	//			numTotalBits, rd.rrange, rd.code)
	for i := numTotalBits; i != 0; i-- {
		rd.rrange = int32(uint32(rd.rrange) >> 1)
		t := int32(uint32(rd.code-rd.rrange) >> 31)
		rd.code -= rd.rrange & (t - 1)
		res = (res << 1) | uint32(1-t)
		//fmt.Printf("rd.decoder.decodeDirectBits() [1]: numTotalBits = %d, Range = %d, Code = %d, result = %d, t = %d\n",
	        //                        numTotalBits, rd.rrange, rd.code, res, t)
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
			//fmt.Printf("rd.decoder.decodeDirectBits() [2]: numTotalBits = %d, Range = %d, Code = %d, result = %d, t = %d\n",
		        //                        numTotalBits, rd.rrange, rd.code, res, t)
		}
	}
	//fmt.Printf("rd.decoder.decodeDirectBits() [3]: numTotalBits = %d, Range = %d, Code = %d, result = %d\n",
	//			numTotalBits, rd.rrange, rd.code, res)
	return
}

func (rd *rangeDecoder) decodeBit(probs []uint16, index uint32) (res uint32, err os.Error) {
	//prob := int32(probs[index])
	prob := probs[index]
	newBound := int32(uint32(rd.rrange)>>kNumBitModelTotalBits) * int32(prob)
	if rd.code^int32(-1<<31) < newBound^int32(-1<<31) {
		rd.rrange = newBound
		probs[index] = prob + (uint16(kBitModelTotal)-prob)>>kNumMoveBits
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 0
		//fmt.Printf("rangeDecoder.decodeBit(): len(probs) = %d, index = %d, prob = %d, probs[index] = %d, "+
		//	"res = %d, newBound = %d, Code = %d, Range = %d\n",
		//	len(probs), index, prob, probs[index], res, newBound, rd.code, rd.rrange)
	} else {
		rd.rrange -= newBound
		rd.code -= newBound
		probs[index] = prob - prob>>kNumMoveBits
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange <<= 8
		}
		res = 1
		//fmt.Printf("rangeDecoder.decodeBit(): len(probs) = %d, index = %d, prob = %d, probs[index] = %d, "+
		//	"res = %d, newBound = %d, Code = %d, Range = %d\n",
		//	len(probs), index, prob, probs[index], res, newBound, rd.code, rd.rrange)
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
