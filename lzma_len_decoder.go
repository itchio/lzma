package lzma

import "os"

type lenDecoder struct {
	choice       []uint16
	lowCoder     []*rangeBitTreeDecoder
	midCoder     []*rangeBitTreeDecoder
	highCoder    *rangeBitTreeDecoder
	numPosStates uint32
}

func newLenDecoder(numPosStates uint32) *lenDecoder {
	ld := &lenDecoder{
		choice:       initBitModels(2),
		lowCoder:     make([]*rangeBitTreeDecoder, kNumPosStatesMax),
		midCoder:     make([]*rangeBitTreeDecoder, kNumPosStatesMax),
		highCoder:    newRangeBitTreeDecoder(kNumHighLenBits),
		numPosStates: numPosStates,
	}
	for i := uint32(0); i < numPosStates; i++ {
		ld.lowCoder[i] = newRangeBitTreeDecoder(kNumLowLenBits)
		ld.midCoder[i] = newRangeBitTreeDecoder(kNumMidLenBits)
	}
	return ld
}

func (ld *lenDecoder) decode(rd *rangeDecoder, posState uint32) (res uint32, err os.Error) {
	i, err := rd.decodeBit(ld.choice, 0)
	if err != nil {
		return
	}
	if i == 0 {
		res, err = ld.lowCoder[posState].decode(rd)
		return
	}
	res = kNumLowLenSymbols
	j, err := rd.decodeBit(ld.choice, 1)
	if err != nil {
		return
	}
	if j == 0 {
		k, err := ld.midCoder[posState].decode(rd)
		if err != nil {
			return
		}
		res += k
		return
	} else {
		l, err := ld.highCoder.decode(rd)
		if err != nil {
			return
		}
		res = res + kNumMidLenSymbols + l
		return
	}
	return
}
