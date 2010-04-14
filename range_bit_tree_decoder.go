package lzma

import "os"

type rangeBitTreeDecoder struct {
	models       []uint16
	numBitLevels uint32
}

func newRangeBitTreeDecoder(numBitLevels uint32) *rangeBitTreeDecoder {
	return &rangeBitTreeDecoder{
		numBitLevels: numBitLevels,
		models:       initBitModels(int(numBitLevels << 1)),
	}
}

func (td *rangeBitTreeDecoder) decode(rd *rangeDecoder) (res uint32, err os.Error) {
	res = 1
	for bitIndex := td.numBitLevels; bitIndex != 0; bitIndex-- {
		bit, err := rd.decodeBit(td.models, int(res))
		if err != nil {
			return
		}
		res = res<<1 + bit
	}
	res -= 1 << td.numBitLevels
	return
}

func (td *rangeBitTreeDecoder) reverseDecode(rd *rangeDecoder) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < td.numBitLevels; bitIndex++ {
		bit, err := rd.decodeBit(td.models, int(index))
		if err != nil {
			return
		}
		index = index<<1 + bit
		res = res | (bit << bitIndex)
	}
	return
}

func reverseDecodeIndex(rd *rangeDecoder, models []uint16, numBitModels uint32, startIndex int) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < numBitModels; bitIndex++ {
		bit, err := rd.decodeBit(models, startIndex+int(index))
		if err != nil {
			return
		}
		index = index<<1 + bit
		res = res | (bit << bitIndex)
	}
	return
}
