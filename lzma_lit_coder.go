package lzma

type litCoder2 struct {
	coders []uint16
}

func newLitCoder2() *litCoder2 {
	return &litCoder2{
		coders: initBitModels(0x300),
	}
}


func (lc2 *litCoder2) decodeNormal(rd *rangeDecoder) byte {
	symbol := uint32(1)
	for symbol < 0x100 {
		i := rd.decodeBit(lc2.coders, symbol)
		symbol = symbol<<1 | i
	}
	return byte(symbol)
}

func (lc2 *litCoder2) decodeWithMatchByte(rd *rangeDecoder, matchByte byte) byte {
	symbol := uint32(1)
	for symbol < 0x100 {
		matchBit := uint32((matchByte >> 7) & 1)
		matchByte = matchByte << 1
		bit := rd.decodeBit(lc2.coders, ((1+matchBit)<<8)+symbol)
		symbol = (symbol << 1) | bit
		if matchBit != bit {
			for symbol < 0x100 {
				i := rd.decodeBit(lc2.coders, symbol)
				symbol = (symbol << 1) | i
			}
			break
		}
	}
	return byte(symbol)
}


func (lc2 *litCoder2) encode(re *rangeEncoder, symbol byte) {
	context := uint32(1)
	for i := 7; i >= 0; i-- {
		bit := (symbol >> uint8(i)) & 1
		re.encode(lc2.coders, context, uint32(bit))
		context = context<<1 | uint32(bit)
	}
}

func (lc2 *litCoder2) encodeMatched(re *rangeEncoder, matchByte, symbol byte) {
	context := uint32(1)
	same := true
	for i := 7; i >= 0; i-- {
		bit := (symbol >> uint8(i)) & 1
		state := context
		if same == true {
			matchBit := (matchByte >> uint8(i)) & 1
			state += (1 + uint32(matchBit)) << 8
			same = false
			if matchBit == bit {
				same = true
			}
		}
		re.encode(lc2.coders, state, uint32(bit))
		context = context<<1 | uint32(bit)
	}
}

func (lc2 *litCoder2) getPrice(matchMode bool, matchByte, symbol byte) uint32 {
	price := uint32(0)
	context := uint32(1)
	i := 7
	if matchMode == true {
		for ; i >= 0; i-- {
			matchBit := (matchByte >> uint8(i)) & 1
			bit := (symbol >> uint8(i)) & 1
			price += getPrice(uint32(lc2.coders[uint32(1+matchBit)<<8+context]), uint32(bit))
			context = context<<1 | uint32(bit)
			if matchBit != bit {
				i--
				break
			}
		}
	}
	for ; i >= 0; i-- {
		bit := (symbol >> uint8(i)) & 1
		price += getPrice(uint32(lc2.coders[context]), uint32(bit))
		context = context<<1 | uint32(bit)
	}
	return price
}


type litCoder struct {
	coders      []*litCoder2
	numPrevBits uint32
	numPosBits  uint32
	posMask     uint32
}

func newLitCoder(numPosBits, numPrevBits uint32) *litCoder {
	numStates := uint32(1 << (numPrevBits + numPosBits))
	lc := &litCoder{
		coders:      make([]*litCoder2, numStates),
		numPrevBits: numPrevBits,
		numPosBits:  numPosBits,
		posMask:     (1 << numPosBits) - 1,
	}
	for i := uint32(0); i < numStates; i++ {
		lc.coders[i] = newLitCoder2()
	}
	return lc
}

// TODO: rename getCoder to getSubCoder or subCoder
// TODO: rename litCoder2 to litSubCoder
func (lc *litCoder) getCoder(pos uint32, prevByte byte) *litCoder2 {
	lc2 := lc.coders[((pos&lc.posMask)<<lc.numPrevBits)+uint32((prevByte&0xff)>>(8-lc.numPrevBits))]
	return lc2
}
