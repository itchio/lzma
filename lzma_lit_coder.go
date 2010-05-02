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
	uMatchByte := uint32(matchByte)
	symbol := uint32(1)
	for symbol < 0x100 {
		matchBit := (uMatchByte >> 7) & 1
		uMatchByte <<= 1
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
	uSymbol := uint32(symbol)
	context := uint32(1)
	for i := uint32(7); int32(i) >= 0; i-- {
		bit := (uSymbol >> i) & 1
		re.encode(lc2.coders, context, bit)
		context = context<<1 | bit
	}
}

func (lc2 *litCoder2) encodeMatched(re *rangeEncoder, matchByte, symbol byte) {
	uMatchByte := uint32(matchByte)
	uSymbol := uint32(symbol)
	context := uint32(1)
	same := true
	for i := uint32(7); int32(i) >= 0; i-- {
		bit := (uSymbol >> i) & 1
		state := context
		if same == true {
			matchBit := (uMatchByte >> i) & 1
			state += (1 + matchBit) << 8
			same = false
			if matchBit == bit {
				same = true
			}
		}
		re.encode(lc2.coders, state, bit)
		context = context<<1 | bit
	}
}

func (lc2 *litCoder2) getPrice(matchMode bool, matchByte, symbol byte) uint32 {
	uMatchByte := uint32(matchByte)
	uSymbol := uint32(symbol)
	price := uint32(0)
	context := uint32(1)
	i := uint32(7)
	if matchMode == true {
		for ; int32(i) >= 0; i-- {
			matchBit := (uMatchByte >> i) & 1
			bit := (uSymbol >> i) & 1
			price += getPrice(lc2.coders[1+matchBit<<8+context], bit)
			context = context<<1 | bit
			if matchBit != bit {
				i--
				break
			}
		}
	}
	for ; int32(i) >= 0; i-- {
		bit := (uSymbol >> i) & 1
		price += getPrice(lc2.coders[context], bit)
		context = context<<1 | bit
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
	numStates := uint32(1) << (numPrevBits + numPosBits)
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
	lc2 := lc.coders[((pos&lc.posMask)<<lc.numPrevBits)+uint32(prevByte>>(8-lc.numPrevBits))]
	return lc2
}
