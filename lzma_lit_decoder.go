package lzma

import "os"

type litDecoder2 struct {
	decoders []uint16
}

func newLitDecoder2() *litDecoder2 {
	return &litDecoder2{
		decoders: initBitModels(0x300),
	}
}

func (ld2 *litDecoder2) decodeNormal(rd *rangeDecoder) (b byte, err os.Error) {
	symbol := uint32(1)
	for symbol < 0x100 {
		i, err := rd.decodeBit(ld2.decoders, symbol)
		if err != nil {
			return
		}
		symbol = symbol<<1 | i
	}
	return byte(symbol), nil
}

func (ld2 *litDecoder2) decodeWithMatchByte(rd *rangeDecoder, matchByte byte) (b byte, err os.Error) {
	symbol := uint32(1)
	for symbol < 0x100 {
		matchBit := uint32((matchByte >> 7) & 1)
		matchByte = matchByte << 1
		bit, err := rd.decodeBit(ld2.decoders, ((1+matchBit)<<8)+symbol)
		if err != nil {
			return
		}
		symbol = (symbol << 1) | bit
		if matchBit != bit {
			for symbol < 0x100 {
				i, err := rd.decodeBit(ld2.decoders, symbol)
				if err != nil {
					return
				}
				symbol = (symbol << 1) | i
			}
			break
		}
	}
	return byte(symbol), nil
}

type litDecoder struct {
	coders      []*litDecoder2
	numPrevBits uint32
	numPosBits  uint32
	posMask     uint32
}

func newLitDecoder(numPosBits, numPrevBits uint32) *litDecoder {
	numStates := uint32(1 << (numPrevBits + numPosBits))
	ld := &litDecoder{
		coders:      make([]*litDecoder2, numStates),
		numPrevBits: numPrevBits,
		numPosBits:  numPosBits,
		posMask:     (1 << numPosBits) - 1,
	}
	for i := uint32(0); i < numStates; i++ {
		ld.coders[i] = newLitDecoder2()
	}
	return ld
}

func (ld *litDecoder) getDecoder(pos uint32, prevByte byte) *litDecoder2 {
	return ld.coders[((pos&ld.posMask)<<ld.numPrevBits)+uint32((prevByte&0xff)>>(8-ld.numPrevBits))]
}
