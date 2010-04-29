package lzma

import (
	"os"
	"fmt"
)

type rangeBitTreeCoder struct {
	models       []uint16 // length(models) is at most 1<<8
	numBitLevels uint32   // max 8 (between 2 or 3 and 8); sould it be a uint8 ?
}

func newRangeBitTreeCoder(numBitLevels uint32) *rangeBitTreeCoder {
	return &rangeBitTreeCoder{
		numBitLevels: numBitLevels,
		models:       initBitModels(1 << numBitLevels),
	}
}

// ---------------- decode --------------------

func (rc *rangeBitTreeCoder) decode(rd *rangeDecoder) (res uint32, err os.Error) {
	res = 1
	for bitIndex := rc.numBitLevels; bitIndex != 0; bitIndex-- {
		bit, err := rd.decodeBit(rc.models, res)
		if err != nil {
			return
		}
		res = res<<1 + bit
	}
	res -= 1 << rc.numBitLevels
	return
}

func (rc *rangeBitTreeCoder) reverseDecode(rd *rangeDecoder) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < rc.numBitLevels; bitIndex++ {
		bit, err := rd.decodeBit(rc.models, index)
		if err != nil {
			return
		}
		index <<= 1
		index += bit
		res = res | (bit << bitIndex)
	}
	return
}

func reverseDecodeIndex(rd *rangeDecoder, models []uint16, startIndex int32, numBitModels uint32) (res uint32, err os.Error) {
	index := uint32(1)
	res = 0
	for bitIndex := uint32(0); bitIndex < numBitModels; bitIndex++ {
		bit, err := rd.decodeBit(models, uint32(startIndex+int32(index)))
		if err != nil {
			return
		}
		index <<= 1
		index += bit
		res = res | (bit << bitIndex)
	}
	return
}

// ---------------- encode --------------------

func (rc *rangeBitTreeCoder) encode(re *rangeEncoder, symbol uint32) (err os.Error) {
	m := uint32(1)
	for bitIndex := rc.numBitLevels; bitIndex != 0; {
		bitIndex--
		bit := (symbol >> bitIndex) & 1
		err = re.encode(rc.models, m, bit)
		if err != nil {
			return
		}
		m = (m << 1) | bit

		fmt.Printf("[0] rc.encode(): symbol = %d, bitIndex = %d, m = %d, bit = %d, rc.numBitLevels = %d\n",
			symbol, bitIndex, m, bit, rc.numBitLevels)

	}
	return
}

func (rc *rangeBitTreeCoder) reverseEncode(re *rangeEncoder, symbol uint32) (err os.Error) {
	m := uint32(1)
	for i := uint32(0); i < rc.numBitLevels; i++ {
		bit := symbol & 1
		err = re.encode(rc.models, m, bit)
		if err != nil {
			return
		}
		m = (m << 1) | bit
		symbol >>= 1
	}
	return
}

func (rc *rangeBitTreeCoder) getPrice(symbol uint32) (res uint32) {
	res = 0
	m := uint32(1)
	for bitIndex := rc.numBitLevels; bitIndex != 0; {
		bitIndex--
		bit := (symbol >> bitIndex) & 1
		res += getPrice(uint32(rc.models[m]), bit)
		m = (m << 1) + bit
	}
	return
}

func (rc *rangeBitTreeCoder) reverseGetPrice(symbol uint32) (res uint32) {
	res = 0
	m := uint32(1)
	for i := rc.numBitLevels; i != 0; i-- {
		bit := symbol & 1
		symbol >>= 1
		res += getPrice(uint32(rc.models[m]), bit)
		m = (m << 1) | bit
	}
	return
}

func reverseGetPriceIndex(models []uint16, startIndex int32, numBitLevels, symbol uint32) (res uint32) {
	res = 0
	m := uint32(1)
	for i := numBitLevels; i != 0; i-- {
		bit := symbol & 1
		symbol >>= 1
		res += getPrice(uint32(models[startIndex+int32(m)]), bit)
		m = (m << 1) | bit
	}
	return
}

func reverseEncodeIndex(re *rangeEncoder, models []uint16, startIndex int32, numBitLevels, symbol uint32) (err os.Error) {
	m := uint32(1)
	for i := uint32(0); i < numBitLevels; i++ {
		bit := symbol & 1
		err = re.encode(models, uint32(startIndex+int32(m)), bit)
		if err != nil {
			return
		}
		m = (m << 1) | bit
		symbol >>= 1
	}
	return
}
