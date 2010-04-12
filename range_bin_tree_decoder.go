package lzma

import "os"

type rangeBinTreeDecoder struct {
	models []uint16
	numBitLevels uint32
}

func newRangeBinTreeDecoder(numBitLevels uint32) (td rangeBinTreeDecoder) {
	td.numBitLevels = numBitLevels
	td.models = initBitModels(int(td.numBitLevels << 1))
	return
}

func (td *rangeBinTreeDecoder) decode(rd rangeDecoder) (res uint32, err os.Error) {
	res = 1
	for bitIndex := td.numBitLevels; bitIndex != 0; bitIndex-- {
		bit, err := rd.decodeBit(td.models, int(res))
		if err != nil {
			return
		}
		res = res << 1 + bit
	}
	res -= 1 << td.numBitLevels
	return
}

func (td *rangeBinTreeDecoder) reverseDecode(rd rangeDecoder) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < td.numBitLevels; bitIndex++ {
		bit, err := rd.decodeBit(td.models, int(index))
		if err != nil {
			return
		}
		index = index << 1 + bit
		res = res | (bit << bitIndex)
	}
	return
}

func reverseDecodeIndex(rd rangeDecoder, models []uint16, numBitModels uint32, startIndex int) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < numBitModels; bitIndex++ {
		bit, err := rd.decodeBit(models, startIndex + int(index))
		if err != nil {
			return
		}
		index = index << 1 + bit
		res = res | (bit << bitIndex)
	}
	return
}
