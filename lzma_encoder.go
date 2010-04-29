package lzma

import (
	"io"
	//"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	BestSpeed          = 1
	BestCompression    = 9
	DefaultCompression = 5
)


type syncPipeReader struct {
	*io.PipeReader
	closeChan chan bool
}

func (sr *syncPipeReader) CloseWithError(err os.Error) os.Error {
	retErr := sr.PipeReader.CloseWithError(err)
	sr.closeChan <- true // finish writer close
	return retErr
}

type syncPipeWriter struct {
	*io.PipeWriter
	closeChan chan bool
}

func (sw *syncPipeWriter) Close() os.Error {
	err := sw.PipeWriter.Close()
	<-sw.closeChan // wait for reader close
	return err
}

func syncPipe() (*syncPipeReader, *syncPipeWriter) {
	r, w := io.Pipe()
	sr := &syncPipeReader{r, make(chan bool, 1)}
	sw := &syncPipeWriter{w, sr.closeChan}
	return sr, sw
}


type compressionLevel struct {
	dictSize        uint32 // d, 1 << dictSize
	fastBytes       uint32 // fb
	litContextBits  uint32 // lc
	litPosStateBits uint32 // lp
	posStateBits    uint32 // pb
	matchFinder     string // mf
	//compressionMode uint32 // a
	//matchCycles     uint32 // mc
}

var levels = []*compressionLevel{
	&compressionLevel{},                        // 0
	&compressionLevel{16, 64, 3, 0, 2, "bt4"},  // 1
	&compressionLevel{18, 64, 3, 0, 2, "bt4"},  // 2
	&compressionLevel{20, 64, 3, 0, 2, "bt4"},  // 3
	&compressionLevel{22, 64, 3, 0, 2, "bt4"},  // 4
	&compressionLevel{23, 128, 3, 0, 2, "bt4"}, // 5
	&compressionLevel{24, 128, 3, 0, 2, "bt4"}, // 6
	&compressionLevel{25, 128, 3, 0, 2, "bt4"}, // 7
	&compressionLevel{26, 255, 3, 0, 2, "bt4"}, // 8
	&compressionLevel{27, 255, 3, 0, 2, "bt4"}, // 9
}

func (cl *compressionLevel) checkValues() os.Error {
	if cl.dictSize < 0 || cl.dictSize > 29 {
		return os.NewError("dictionary size out of range: " + string(cl.dictSize))
	}
	if cl.fastBytes < 5 || cl.fastBytes > 273 {
		return os.NewError("number of fast bytes out of range: " + string(cl.fastBytes))
	}
	if cl.litContextBits < 0 || cl.litContextBits > 8 {
		return os.NewError("number of literal context bits out of range: " + string(cl.litContextBits))
	}
	if cl.litPosStateBits < 0 || cl.litPosStateBits > 4 {
		return os.NewError("number of literal position bits out of range: " + string(cl.litPosStateBits))
	}
	if cl.posStateBits < 0 || cl.posStateBits > 4 {
		return os.NewError("number of position bits out of range: " + string(cl.posStateBits))
	}
	if cl.matchFinder != "bt2" && cl.matchFinder != "bt4" { // there are also bt3 and hc4, but will implement them later
		return os.NewError("unsuported match finder: " + cl.matchFinder)
	}
	return nil
}


var gFastPos []byte = make([]byte, 1<<11)

// should be called in the encoder's contructor
func initGFastPos() {
	kFastSlots := 22
	c := 2
	gFastPos[0] = 0
	gFastPos[1] = 1
	for slotFast := 2; slotFast < kFastSlots; slotFast++ {
		k := 1 << uint(slotFast>>1-1)
		for j := 0; j < k; j, c = j+1, c+1 {
			gFastPos[c] = byte(slotFast)
		}
	}
}

func getPosSlot(pos uint32) uint32 {
	if pos < 1<<11 {
		return uint32(gFastPos[pos])
	}
	if pos < 1<<21 {
		return uint32(gFastPos[pos>>10] + 20)
	}
	return uint32(gFastPos[pos>>20] + 40)
}

func getPosSlot2(pos uint32) uint32 {
	if pos < 1<<17 {
		return uint32(gFastPos[pos>>6] + 12)
	}
	if pos < 1<<27 {
		return uint32(gFastPos[pos>>16] + 32)
	}
	return uint32(gFastPos[pos>>26] + 52)
}


type optimal struct {
	state,
	posPrev2,
	backPrev2,
	price,
	posPrev,
	backPrev,
	backs0,
	backs1,
	backs2,
	backs3 uint32

	prev1IsChar,
	prev2 bool
}

func (o *optimal) makeAsChar() {
	o.backPrev = 0xFFFFFFFF
	o.prev1IsChar = false
}

func (o *optimal) makeAsShortRep() {
	o.backPrev = 0
	o.prev1IsChar = false
}

func (o *optimal) isShortRep() bool {
	if o.backPrev == 0 {
		return true
	}
	return false
}

//func (o *optimal) str1() string {
//	return fmt.Sprintf("o.state = %d, o.posPrev2 = %d, o.backPrev2 = %d, o.price = %d, o.posPrev = %d, o.backPrev = %d",
//		o.state, o.posPrev2, o.backPrev2, o.price, o.posPrev, int32(o.backPrev))
//}

//func (o *optimal) str2() string {
//	return fmt.Sprintf("o.backs0 = %d, o.backs1 = %d, o.backs2 = %d, o.backs3 = %d, o.prev1IsChar = %t, o.prev2 = %t",
//		o.backs0, o.backs1, o.backs2, o.backs3, o.prev1IsChar, o.prev2)
//}


const (
	eMatchFinderTypeBT2  = 0
	eMatchFinderTypeBT4  = 1
	kInfinityPrice       = 0xFFFFFFF
	kDefaultDicLogSize   = 22
	kNumFastBytesDefault = 0x20
	kNumLenSpecSymbols   = kNumLowLenSymbols + kNumMidLenSymbols
	kNumOpts             = 1 << 12
)

type encoder struct {
	// i/o, range encoder and match finder
	re *rangeEncoder // w
	mf *lzBinTree    // r

	cl           *compressionLevel
	size         int64
	writeEndMark bool // eos

	optimum []*optimal

	isMatch    []uint16
	isRep      []uint16
	isRepG0    []uint16
	isRepG1    []uint16
	isRepG2    []uint16
	isRep0Long []uint16

	posSlotCoders []*rangeBitTreeCoder

	posCoders     []uint16
	posAlignCoder *rangeBitTreeCoder

	lenCoder         *lenPriceTableCoder
	repMatchLenCoder *lenPriceTableCoder

	litCoder *litCoder

	matchDistances []uint32

	longestMatchLen uint32
	distancePairs   uint32

	additionalOffset uint32

	optimumEndIndex     uint32
	optimumCurrentIndex uint32

	longestMatchFound bool

	posSlotPrices   []uint32
	distancesPrices []uint32
	alignPrices     []uint32
	alignPriceCount uint32

	distTableSize uint32

	posStateMask uint32

	nowPos   int64
	finished bool

	matchFinderType uint32

	state           uint32
	prevByte        byte
	repDistances    []uint32
	matchPriceCount uint32

	reps    []uint32
	repLens []uint32
	backRes uint32
}

// signature: c | go | cs
func (z *encoder) readMatchDistances() (lenRes uint32, err os.Error) {
	lenRes = 0
	z.distancePairs, err = z.mf.getMatches(z.matchDistances)

	//fmt.Printf("[0] z.readMatchDistances(): z.distancePairs = %d\n", z.distancePairs)
	/*
		zr198++
		if zr198 == 198 {
			panic("zr is 198")
		}
	*/

	if err != nil {
		return
	}
	if z.distancePairs > 0 {
		lenRes = z.matchDistances[z.distancePairs-2]
		if lenRes == z.cl.fastBytes {
			lenRes += z.mf.iw.getMatchLen(int32(lenRes)-1, z.matchDistances[z.distancePairs-1], kMatchMaxLen-lenRes)
		}
	}
	z.additionalOffset++
	return
}

// signature: c | go | cs
func (z *encoder) movePos(num uint32) (err os.Error) {
	if num > 0 {
		z.additionalOffset += num
		err = z.mf.skip(num)
	}
	return
}

// signature: c | go | cs
func (z *encoder) getPureRepPrice(repIndex, state, posState uint32) (price uint32) {
	if repIndex == 0 {
		price = getPrice0(uint32(z.isRepG0[state]))
		price += getPrice1(uint32(z.isRep0Long[state<<kNumPosStatesBitsMax+posState]))
	} else {
		price = getPrice1(uint32(z.isRepG0[state]))
		if repIndex == 1 {
			price += getPrice0(uint32(z.isRepG1[state]))
		} else {
			price += getPrice1(uint32(z.isRepG1[state]))
			price += getPrice(uint32(z.isRepG2[state]), repIndex-2)
		}
	}
	return
}

// signature: c | go | cs
func (z *encoder) getRepPrice(repIndex, length, state, posState uint32) (price uint32) {
	price = z.repMatchLenCoder.getPrice(length-kMatchMinLen, posState)
	price += z.getPureRepPrice(repIndex, state, posState)

	//fmt.Printf("[0] z.getRepPrice(): repIndex = %d, length = %d, state = %d, posState = %d, price = %d\n", repIndex, length, state, posState, price)

	return
}

// singature: c | go | cs
func (z *encoder) getPosLenPrice(pos, length, posState uint32) (price uint32) {
	lenToPosState := getLenToPosState(length)
	if pos < kNumFullDistances {
		price = z.distancesPrices[lenToPosState*kNumFullDistances+pos]

		//fmt.Printf("[0] z.getPosLenPrice(): pos = %d, length = %d, posState = %d, price = %d, lenToPosState = %d, kNumFullDistances = %d, index = %d\n",
		//	pos, length, posState, price, lenToPosState, kNumFullDistances, lenToPosState*kNumFullDistances+pos)

	} else {
		price = z.posSlotPrices[lenToPosState<<kNumPosSlotBits+getPosSlot2(pos)] + z.alignPrices[pos&kAlignMask]

		//fmt.Printf("[1] z.getPosLenPrice(): pos = %d, length = %d, posState = %d, price = %d, lenToPosState = %d, kNumPosSlotBits = %d, kAlignMask = %d\n",
		//	pos, length, posState, price, lenToPosState, kNumPosSlotBits, kAlignMask)
		//fmt.Printf("[2] z.getPosLenPrice(): index1 = %d, index2 = %d, z.posSlotPrices[index1] = %d, z.alignPrices[index2] = %d\n",
		//	lenToPosState<<kNumPosSlotBits+getPosSlot2(pos), pos&kAlignMask,
		//	z.posSlotPrices[lenToPosState<<kNumPosSlotBits+getPosSlot2(pos)], z.alignPrices[pos&kAlignMask])

	}

	//fmt.Printf("[3] z.getPosLenPrice(): pos = %d, length = %d, posState = %d, price = %d, lenToPosState = %d\n",
	//	pos, length, posState, price, lenToPosState)
	//fmt.Printf("[4] z.getPosLenPrice(): kNumFullDistances = %d, kNumPosSlotBits = %d, kAlignMask = %d, kMatchMinLen = %d\n",
	//	kNumFullDistances, kNumPosSlotBits, kAlignMask, kMatchMinLen)

	price += z.lenCoder.getPrice(length-kMatchMinLen, posState)

	//fmt.Printf("[5] z.getPosLenPrice(): price = %d\n", price)

	return
}

// signature: c | go | cs
func (z *encoder) getRepLen1Price(state, posState uint32) uint32 {
	return getPrice0(uint32(z.isRepG0[state])) +
		getPrice0(uint32(z.isRep0Long[state<<kNumPosStatesBitsMax+posState]))
}

// signature: c | go | cs
func (z *encoder) backward(cur uint32) uint32 {
	z.optimumEndIndex = cur
	posMem := z.optimum[cur].posPrev
	backMem := z.optimum[cur].backPrev

	//fmt.Printf("[0] z.backward(): cur = %d, posMem = %d, backMem = %d\n", cur, int32(posMem), int32(backMem))

	tmp := uint32(1) // execute loop at least once (do-while)
	for ; tmp > 0; tmp = cur {
		if z.optimum[cur].prev1IsChar == true {
			z.optimum[posMem].makeAsChar()
			z.optimum[posMem].posPrev = posMem - 1

			//fmt.Printf("[1] z.backward(): posMem = %d, z.optimum.[posMem].backPrev = %d, z.optimum[posMem].posPrev = %d\n",
			//	posMem, int32(z.optimum[posMem].backPrev), z.optimum[posMem].posPrev)

			if z.optimum[cur].prev2 == true {
				z.optimum[posMem-1].prev1IsChar = false
				z.optimum[posMem-1].posPrev = z.optimum[cur].posPrev2
				z.optimum[posMem-1].backPrev = z.optimum[cur].backPrev2

				//fmt.Printf("[2] z.backward(): posMem-1 = %d, z.optimum.[posMem-1].backPrev = %d, z.optimum[posMem-1].posPrev = %d\n",
				//	posMem-1, z.optimum[posMem-1].backPrev, z.optimum[posMem-1].posPrev)

			}
		}
		posPrev := posMem
		backCur := backMem
		backMem = z.optimum[posPrev].backPrev
		posMem = z.optimum[posPrev].posPrev
		z.optimum[posPrev].backPrev = backCur
		z.optimum[posPrev].posPrev = cur
		cur = posPrev

		//fmt.Printf("[3] z.backward(): posPrev = %d, backCur = %d, backMem = %d, posMem = %d, cur = %d\n",
		//	int32(posPrev), int32(backCur), int32(backMem), int32(posMem), cur)

	}
	z.backRes = z.optimum[0].backPrev
	z.optimumCurrentIndex = z.optimum[0].posPrev

	//fmt.Printf("[4] z.backward(): z.backRes = %d, z.optimumCurrentIndex = %d\n", int32(z.backRes), z.optimumCurrentIndex)

	return z.optimumCurrentIndex
}

func (z *encoder) getOptimum(position uint32) (res uint32, err os.Error) {

	//fmt.Printf("[0] z.getOptimum(): position = %d, z.optimumEndIndex = %d, z.optimumCurrentIndex = %d, z.backRes = %d\n",
	//	position, z.optimumEndIndex, z.optimumCurrentIndex, int32(z.backRes))

	if z.optimumEndIndex != z.optimumCurrentIndex {
		lenRes := z.optimum[z.optimumCurrentIndex].posPrev - z.optimumCurrentIndex
		z.backRes = z.optimum[z.optimumCurrentIndex].backPrev
		z.optimumCurrentIndex = z.optimum[z.optimumCurrentIndex].posPrev
		res = lenRes

		//fmt.Printf("[1] z.getOptimum(): z.optimumEndIndex = %d, z.optimumCurrentIndex = %d, z.backRes = %d, lenRes = %d\n",
		//	z.optimumEndIndex, z.optimumCurrentIndex, int32(z.backRes), lenRes)

		return
	}

	z.optimumEndIndex = 0
	z.optimumCurrentIndex = 0
	var lenMain uint32
	var distancePairs uint32

	//fmt.Printf("[2] z.getOptimum(): position = %d, z.longestMatchFound = %t\n", position, z.longestMatchFound)

	if z.longestMatchFound == false {
		lenMain, err = z.readMatchDistances()
		if err != nil {
			return
		}

		//fmt.Printf("[3] z.getOptimum(): position = %d, lenMain = %d\n", position, lenMain)

	} else {
		lenMain = z.longestMatchLen
		z.longestMatchFound = false
	}
	distancePairs = z.distancePairs
	availableBytes := z.mf.iw.getNumAvailableBytes() + 1

	//fmt.Printf("[4] z.getOptimum(): position = %d, lenMain = %d, distancePairs = %d, availableBytes = %d\n", position, lenMain, distancePairs, availableBytes)

	if availableBytes < 2 {
		z.backRes = 0xFFFFFFFF
		res = 1

		//fmt.Printf("[5] z.getOptimum(): availableBytes = %d, z.backRes = %d, result = %d\n", availableBytes, int32(z.backRes), res)

		return
	}

	if availableBytes > kMatchMaxLen {
		availableBytes = kMatchMaxLen
	}
	repMaxIndex := uint32(0)
	for i := uint32(0); i < kNumRepDistances; i++ {
		z.reps[i] = z.repDistances[i]
		z.repLens[i] = z.mf.iw.getMatchLen(0-1, z.reps[i], kMatchMaxLen)
		if z.repLens[i] > z.repLens[repMaxIndex] {
			repMaxIndex = i
		}
	}
	if z.repLens[repMaxIndex] >= z.cl.fastBytes {
		z.backRes = repMaxIndex
		lenRes := z.repLens[repMaxIndex]
		res = lenRes
		err = z.movePos(lenRes - 1)

		//fmt.Printf("[6] z.getOptimum(): availableBytes = %d, z.backRes = %d, repMaxIndex = %d, result = %d\n",
		//	availableBytes, int32(z.backRes), repMaxIndex, res)

		return
	}

	if lenMain >= z.cl.fastBytes {
		z.backRes = z.matchDistances[distancePairs-1] + kNumRepDistances
		res = lenMain
		err = z.movePos(lenMain - 1)

		//fmt.Printf("[7] z.getOptimum(): availableBytes = %d, z.backRes = %d, repMaxIndex = %d, lenMain = %d, distancePairs = %d, result = %d\n",
		//	availableBytes, z.backRes, repMaxIndex, lenMain, distancePairs, res)

		return
	}

	curByte := z.mf.iw.getIndexByte(0 - 1)
	//fmt.Printf("[7.5] z.getOptimum(): iw.getIndexByte() with arg %d called\n", -1)
	matchByte := z.mf.iw.getIndexByte(0 - int32(z.repDistances[0]) - 1 - 1)
	//fmt.Printf("[7.6] z.getOptimum(): iw.getIndexByte() with arg %d called\n", 0-int32(z.repDistances[0])-1-1)
	if lenMain < 2 && curByte != matchByte && z.repLens[repMaxIndex] < 2 {
		z.backRes = 0xFFFFFFFF
		res = 1

		//fmt.Printf("[8] z.getOptimum(): availableBytes = %d, z.backRes = %d, repMaxIndex = %d, lenMain = %d, curByte = %d, matchByte = %d, result = %d\n",
		//	availableBytes, int32(z.backRes), repMaxIndex, lenMain, int8(curByte), int8(matchByte), res)

		return
	}

	z.optimum[0].state = z.state
	posState := position & z.posStateMask
	z.optimum[1].price = getPrice0(uint32(z.isMatch[z.state<<kNumPosStatesBitsMax+posState])) +
		z.litCoder.getCoder(position, z.prevByte).getPrice(!stateIsCharState(z.state), matchByte, curByte)
	z.optimum[1].makeAsChar()

	matchPrice := getPrice1(uint32(z.isMatch[z.state<<kNumPosStatesBitsMax+posState]))
	repMatchPrice := matchPrice + getPrice1(uint32(z.isRep[z.state]))
	if matchByte == curByte {
		shortRepPrice := repMatchPrice + z.getRepLen1Price(z.state, posState)
		if shortRepPrice < z.optimum[1].price {
			z.optimum[1].price = shortRepPrice
			z.optimum[1].makeAsShortRep()
		}
	}

	lenEnd := z.repLens[repMaxIndex]
	if lenMain > lenEnd {
		lenEnd = lenMain
	}
	if lenEnd < 2 {
		z.backRes = z.optimum[1].backPrev
		res = 1

		//fmt.Printf("[9] z.getOptimum(): z.backRes = %d, lenEnd = %d, lenMain = %d, result = %d\n",
		//	int32(z.backRes), lenEnd, lenMain, res)

		return
	}

	z.optimum[1].posPrev = 0
	z.optimum[0].backs0 = z.reps[0]
	z.optimum[0].backs1 = z.reps[1]
	z.optimum[0].backs2 = z.reps[2]
	z.optimum[0].backs3 = z.reps[3]

	//fmt.Printf("[9.06] z.getOptimum(): %s\n", z.optimum[0].str1())
	//fmt.Printf("[9.07] z.getOptimum(): %s\n", z.optimum[0].str2())

	length := lenEnd
dowhile1:
	z.optimum[length].price = kInfinityPrice
	if length--; length >= 2 {
		// out of about 30 while's, only 3 of them that can't be expressed without a goto
		// statement; the other occurences of goto explain why code duplicaions isn't an option
		goto dowhile1
	}

	for i := uint32(0); i < kNumRepDistances; i++ {
		repLen := z.repLens[i]
		if repLen < 2 {
			continue
		}
		price := repMatchPrice + z.getPureRepPrice(i, z.state, posState)
	dowhile2:
		curAndLenPrice := price + z.repMatchLenCoder.getPrice(repLen-2, posState)

		//fmt.Printf("[9.11] z.getOptimum(): curAndLenPrice = %d, price = %d, repLen = %d, posState = %d, z.state = %d\n",
		//	curAndLenPrice, price, repLen, posState, z.state)

		optimum := z.optimum[repLen]
		if curAndLenPrice < optimum.price {
			optimum.price = curAndLenPrice
			optimum.posPrev = 0
			optimum.backPrev = i
			optimum.prev1IsChar = false

			//fmt.Printf("[9.15] z.getOptimum(): %s\n", optimum.str1())
			//fmt.Printf("[9.16] z.getOptimum(): %s\n", optimum.str2())
			//fmt.Printf("[9.17] z.getOptimum(): curAndLenPrice = %d, i = %d, repLen = %d, price = %d, posState = %d, z.state = %d\n",
			//	curAndLenPrice, i, repLen, price, posState, z.state)

		}
		if repLen--; repLen >= 2 {
			goto dowhile2
		}
	}

	normalMatchPrice := matchPrice + getPrice0(uint32(z.isRep[z.state]))
	length = 2
	if z.repLens[0] >= 2 {
		length = z.repLens[0] + 1
	}
	if length <= lenMain {
		offs := uint32(0)
		for length > z.matchDistances[offs] {
			offs += 2
		}
		for ; ; length++ {
			distance := z.matchDistances[offs+1]
			curAndLenPrice := normalMatchPrice + z.getPosLenPrice(distance, length, posState)
			optimum := z.optimum[length]
			if curAndLenPrice < optimum.price {
				optimum.price = curAndLenPrice
				optimum.posPrev = 0
				optimum.backPrev = distance + kNumRepDistances
				optimum.prev1IsChar = false

				//fmt.Printf("[9.25] z.getOptimum(): %s\n", optimum.str1())
				//fmt.Printf("[9.26] z.getOptimum(): %s\n", optimum.str2())
				//fmt.Printf("[9.27] z.getOptimum(): distance = %d, curAndLenPrice = %d, normalMatchPrice = %d, length = %d, posState = %d, offs = %d\n",
				//	distance, curAndLenPrice, normalMatchPrice, length, posState, offs)
				//fmt.Printf("[9.28] z.getOptimum(): lenMain = %d, distancePairs = %d, z.state = %d\n", lenMain, distancePairs, z.state)

			}
			if length == z.matchDistances[offs] {
				offs += 2
				if offs == distancePairs {
					break
				}
			}
		}
	}

	cur := uint32(0)
	for {
		cur++
		if cur == lenEnd {
			res = z.backward(cur)

			//fmt.Printf("[10] z.getOptimum(): cur = %d, lenEnd = %d, result = %d\n", cur, lenEnd, res)

			return
		}

		newLen, err := z.readMatchDistances()
		if err != nil {
			return
		}
		distancePairs = z.distancePairs
		if newLen >= z.cl.fastBytes {
			z.longestMatchLen = newLen
			z.longestMatchFound = true
			res = z.backward(cur)

			//fmt.Printf("[11] z.getOptimum(): cur = %d, lenEnd = %d, newLen = %d, distancePairs = %d, result = %d\n",
			//	cur, lenEnd, newLen, distancePairs, res)

			return
		}

		position++
		posPrev := z.optimum[cur].posPrev
		var state uint32
		if z.optimum[cur].prev1IsChar == true {
			posPrev--
			if z.optimum[cur].prev2 == true {
				state = z.optimum[z.optimum[cur].posPrev2].state
				if z.optimum[cur].backPrev2 < kNumRepDistances {
					state = stateUpdateRep(state)
				} else {
					state = stateUpdateMatch(state)
				}
			} else {
				state = z.optimum[posPrev].state
			}
			state = stateUpdateChar(state)
		} else {
			state = z.optimum[posPrev].state
		}
		if posPrev == cur-1 {
			if z.optimum[cur].isShortRep() == true {
				state = stateUpdateShortRep(state)
			} else {
				state = stateUpdateChar(state)
			}
		} else {
			var pos uint32
			if z.optimum[cur].prev1IsChar == true && z.optimum[cur].prev2 == true {
				posPrev = z.optimum[cur].posPrev2
				pos = z.optimum[cur].backPrev2
				state = stateUpdateRep(state)
			} else {
				pos = z.optimum[cur].backPrev
				if pos < kNumRepDistances {
					state = stateUpdateRep(state)
				} else {
					state = stateUpdateMatch(state)
				}
			}
			opt := z.optimum[posPrev]
			if pos < kNumRepDistances {
				if pos == 0 {
					z.reps[0] = opt.backs0
					z.reps[1] = opt.backs1
					z.reps[2] = opt.backs2
					z.reps[3] = opt.backs3
				} else if pos == 1 {
					z.reps[0] = opt.backs1
					z.reps[1] = opt.backs0
					z.reps[2] = opt.backs2
					z.reps[3] = opt.backs3
				} else if pos == 2 {
					z.reps[0] = opt.backs2
					z.reps[1] = opt.backs0
					z.reps[2] = opt.backs1
					z.reps[3] = opt.backs3
				} else {
					z.reps[0] = opt.backs3
					z.reps[1] = opt.backs0
					z.reps[2] = opt.backs1
					z.reps[3] = opt.backs2
				}
			} else {
				z.reps[0] = pos - kNumRepDistances
				z.reps[1] = opt.backs0
				z.reps[2] = opt.backs1
				z.reps[3] = opt.backs2
			}
		}
		z.optimum[cur].state = state
		z.optimum[cur].backs0 = z.reps[0]
		z.optimum[cur].backs1 = z.reps[1]
		z.optimum[cur].backs2 = z.reps[2]
		z.optimum[cur].backs3 = z.reps[3]
		curPrice := z.optimum[cur].price

		//fmt.Printf("[11.09] z.getOptimum(): %s\n", z.optimum[cur].str1())
		//fmt.Printf("[11.10] z.getOptimum(): %s\n", z.optimum[cur].str2())

		curByte = z.mf.iw.getIndexByte(0 - 1)
		//fmt.Printf("[11.14] z.getOptimum(): iw.getIndexByte() with arg %d called\n", -1)
		matchByte = z.mf.iw.getIndexByte(0 - int32(z.reps[0]) - 1 - 1)
		//fmt.Printf("[11.15] z.getOptimum(): iw.getIndexByte() with arg %d called\n", 0-int32(z.reps[0])-1-1)
		posState = position & z.posStateMask
		curAnd1Price := curPrice + getPrice0(uint32(z.isMatch[state<<kNumPosStatesBitsMax+posState])) +
			z.litCoder.getCoder(position, z.mf.iw.getIndexByte(0-2)).getPrice(!stateIsCharState(state), matchByte, curByte)
			//fmt.Printf("[11.18] z.getOptimum(): iw.getIndexByte() with arg %d called\n", -2)

		nextOptimum := z.optimum[cur+1]
		nextIsChar := false

		//fmt.Printf("[11.20] z.getOptimum(): %s\n", nextOptimum.str1())
		//fmt.Printf("[11.21] z.getOptimum(): %s\n", nextOptimum.str2())
		//fmt.Printf("[11.22] z.getOptimum(): curAnd1Price = %d, curPrice = %d, state = %d, posState = %d, position = %d, matchByte = %d, curByte = %d\n",
		//	curAnd1Price, curPrice, state, posState, position, int8(matchByte), int8(curByte))

		if curAnd1Price < nextOptimum.price {
			nextOptimum.price = curAnd1Price
			nextOptimum.posPrev = cur
			nextOptimum.makeAsChar()
			nextIsChar = true

			//fmt.Printf("[12] z.getOptimum(): %s\n", nextOptimum.str1())
			//fmt.Printf("[13] z.getOptimum(): %s\n", nextOptimum.str2())
			//fmt.Printf("[14] z.getOptimum(): cur = %d, curAnd1Price = %d, curByte = %d, matchByte = %d, posState = %d\n",
			//	cur, curAnd1Price, int8(curByte), int8(matchByte), posState)

		}

		matchPrice = curPrice + getPrice1(uint32(z.isMatch[state<<kNumPosStatesBitsMax+posState]))
		repMatchPrice = matchPrice + getPrice1(uint32(z.isRep[state]))
		if matchByte == curByte && !(nextOptimum.posPrev < cur && nextOptimum.backPrev == 0) {
			shortRepPrice := repMatchPrice + z.getRepLen1Price(state, posState)
			if shortRepPrice <= nextOptimum.price {
				nextOptimum.price = shortRepPrice
				nextOptimum.posPrev = cur
				nextOptimum.makeAsShortRep()
				nextIsChar = true

				//fmt.Printf("[15] z.getOptimum(): %s\n", nextOptimum.str1())
				//fmt.Printf("[16] z.getOptimum(): %s\n", nextOptimum.str2())
				//fmt.Printf("[17] z.getOptimum(): cur = %d, curAnd1Price = %d, curByte = %d, matchByte = %d, posState = %d, matchPrice = %d, "+
				//	"repMatchPrice = %d\n",
				//	cur, curAnd1Price, int8(curByte), int8(matchByte), posState, matchPrice, repMatchPrice)

			}
		}

		availableBytesFull := z.mf.iw.getNumAvailableBytes() + 1
		availableBytesFull = minUInt32(kNumOpts-1-cur, availableBytesFull)
		availableBytes = availableBytesFull
		if availableBytes < 2 {
			continue
		}
		if availableBytes > z.cl.fastBytes {
			availableBytes = z.cl.fastBytes
		}
		if nextIsChar == false && matchByte != curByte {
			t := minUInt32(availableBytesFull-1, z.cl.fastBytes)
			lenTest2 := z.mf.iw.getMatchLen(0, z.reps[0], t)
			if lenTest2 >= 2 {
				state2 := stateUpdateChar(state)
				posStateNext := (position + 1) & z.posStateMask
				nextRepMatchPrice := curAnd1Price + getPrice1(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext])) +
					getPrice1(uint32(z.isRep[state2]))
				offset := cur + 1 + lenTest2
				for lenEnd < offset {
					lenEnd++
					z.optimum[lenEnd].price = kInfinityPrice
				}
				curAndLenPrice := nextRepMatchPrice + z.getRepPrice(0, lenTest2, state2, posStateNext)
				optimum := z.optimum[offset]
				if curAndLenPrice < optimum.price {
					optimum.price = curAndLenPrice
					optimum.posPrev = cur + 1
					optimum.backPrev = 0
					optimum.prev1IsChar = true
					optimum.prev2 = false

					//fmt.Printf("[18] z.getOptimum(): %s\n", optimum.str1())
					//fmt.Printf("[19] z.getOptimum(): %s\n", optimum.str2())
					//fmt.Printf("[20] z.getOptimum(): cur = %d, curAndLenPrice = %d, offset = %d, lenEnd = %d, nextRepMatchPrice = %d, "+
					//	"posStateNext = %d, lenTest2 = %d, state2 = %d\n",
					//	cur, curAndLenPrice,
					//	offset, lenEnd, nextRepMatchPrice, posStateNext, lenTest2, state2)

				}
			}
		}

		startLen := uint32(2)
		for repIndex := uint32(0); repIndex < kNumRepDistances; repIndex++ {
			lenTest := z.mf.iw.getMatchLen(0-1, z.reps[repIndex], availableBytes)
			if lenTest < 2 {
				continue
			}
			lenTestTemp := lenTest
		dowhile3:
			for lenEnd < cur+lenTest {
				lenEnd++
				z.optimum[lenEnd].price = kInfinityPrice
			}
			curAndLenPrice := repMatchPrice + z.getRepPrice(repIndex, lenTest, state, posState)
			optimum := z.optimum[cur+lenTest]
			if curAndLenPrice < optimum.price {
				optimum.price = curAndLenPrice
				optimum.posPrev = cur
				optimum.backPrev = repIndex
				optimum.prev1IsChar = false

				//fmt.Printf("[21] z.getOptimum(): %s\n", optimum.str1())
				//fmt.Printf("[22] z.getOptimum(): %s\n", optimum.str2())
				//fmt.Printf("[23] z.getOptimum(): cur = %d, curAndLenPrice = %d, repMatchPrice = %d, repIndex = %d, lenTest = %d, state = %d, "+
				//	"posState = %d, lenEnd = %d\n",
				//	cur, curAndLenPrice, repMatchPrice, repIndex, lenTest, state, posState, lenEnd)

			}
			if lenTest--; lenTest >= 2 {
				goto dowhile3
			}

			lenTest = lenTestTemp
			if repIndex == 0 {
				startLen = lenTest + 1
			}

			if lenTest < availableBytesFull {
				t := minUInt32(availableBytesFull-1-lenTest, z.cl.fastBytes)
				lenTest2 := z.mf.iw.getMatchLen(int32(lenTest), z.reps[repIndex], t)
				if lenTest2 >= 2 {
					state2 := stateUpdateRep(state)
					posStateNext := (position + lenTest) & z.posStateMask
					curAndLenCharPrice := repMatchPrice + z.getRepPrice(repIndex, lenTest, state, posState) +
						getPrice0(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext])) +
						z.litCoder.getCoder(position+lenTest, z.mf.iw.getIndexByte(int32(lenTest)-1-1)).getPrice(
							true, z.mf.iw.getIndexByte(int32(lenTest)-1-(int32(z.reps[repIndex]+1))), z.mf.iw.getIndexByte(int32(lenTest)-1))
						/*
							//==========================
							at1 := z.getRepPrice(repIndex, lenTest, state, posState)
							at2 := getPrice0(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext]))
							ec2 := z.litCoder.getCoder(position+lenTest, z.mf.iw.getIndexByte(int32(lenTest)-1-1))
							fmt.Printf("[23.20] z.getOptimum(): iw.getIndexByte() with arg %d called\n", int32(lenTest)-1-1)
							at3 := ec2.getPrice(true,
								z.mf.iw.getIndexByte(int32(lenTest)-1-(int32(z.reps[repIndex]+1))),
								z.mf.iw.getIndexByte(int32(lenTest)-1))
							curAndLenCharPrice := repMatchPrice + at1 + at2 + at3
							fmt.Printf("[23.21] z.getOptimum(): iw.getIndexByte() with arg %d called\n", int32(lenTest)-1-(int32(z.reps[repIndex]+1)))
							fmt.Printf("[23.22] z.getOptimum(): iw.getIndexByte() with arg %d called\n", int32(lenTest)-1)
							//==========================
						*/
						//fmt.Printf("[23.29] z.getOptimum(): curAndLenCharPrice = %d\n", curAndLenCharPrice)

					state2 = stateUpdateChar(state2)
					posStateNext = (position + lenTest + 1) & z.posStateMask
					nextMatchPrice := curAndLenCharPrice + getPrice1(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext]))
					nextRepMatchPrice := nextMatchPrice + getPrice1(uint32(z.isRep[state2]))

					offset := lenTest + 1 + lenTest2

					//fmt.Printf("[23.40] z.getOptimum(): offset = %d, lenTest = %d, lenTest2 = %d, nextRepMatchPrice = %d, nextMatchPrice = %d, "+
					//	"posStateNext = %d, state2 = %d, lenEnd = %d, cur = %d\n",
					//	offset, lenTest,
					//	lenTest2, nextRepMatchPrice, nextMatchPrice, posStateNext, state2, lenEnd, cur)

					for lenEnd < cur+offset {
						lenEnd++
						z.optimum[lenEnd].price = kInfinityPrice

						//fmt.Printf("[23.41] z.getOptimum(): lenEnd = %d, cur = %d, offset = %d, kInfinityPrice = %d, "+
						//	"z.optimum[lenEnd].price = %d\n",
						//	lenEnd, cur, offset, kInfinityPrice, z.optimum[lenEnd].price)

					}
					curAndLenPrice := nextRepMatchPrice + z.getRepPrice(0, lenTest2, state2, posStateNext)
					optimum := z.optimum[cur+offset]

					//fmt.Printf("[23.41] z.getOptimum(): curAndLenPrice = %d, nextRepMatchPrice = %d, lenTest2 = %d, state2 = %d, posStateNext = %d, "+
					//	"cur = %d, offset = %d\n",
					//	curAndLenPrice, nextRepMatchPrice, lenTest2, state2, posStateNext, cur, offset)

					if curAndLenPrice < optimum.price {
						optimum.price = curAndLenPrice
						optimum.posPrev = cur + lenTest + 1
						optimum.backPrev = 0
						optimum.prev1IsChar = true
						optimum.prev2 = true
						optimum.posPrev2 = cur
						optimum.backPrev2 = repIndex

						//fmt.Printf("[24] z.getOptimum(): %s\n", optimum.str1())
						//fmt.Printf("[25] z.getOptimum(): %s\n", optimum.str2())
						//fmt.Printf("[26] z.getOptimum(): cur = %d, lenTest = %d, curAndLenPrice = %d, repIndex = %d, nextRepMatchPrice = %d, "+
						//	"nextMatchPrice = %d\n",
						//	cur, lenTest, curAndLenPrice, repIndex, nextRepMatchPrice, nextMatchPrice)
						//fmt.Printf("[27] z.getOptimum(): lenTest2 = %d, state2 = %d, posStateNext = %d, curAndLenCharPrice = %d, state = %d, "+
						//	"posState = %d\n",
						//	lenTest2, state2, posStateNext, curAndLenCharPrice, state, posState)
						//fmt.Printf("[28] z.getOptimum(): position = %d, availableBytesFull = %d, t = %d, offset = %d\n",
						//	position, availableBytesFull, t, offset)

					}
				}
			}
		}

		if newLen > availableBytes {
			newLen = availableBytes
			for distancePairs = 0; newLen > z.matchDistances[distancePairs]; distancePairs += 2 {
				// empty loop
			}
			z.matchDistances[distancePairs] = newLen
			distancePairs += 2
		}
		if newLen >= startLen {
			normalMatchPrice = matchPrice + getPrice0(uint32(z.isRep[state]))
			for lenEnd < cur+newLen {
				lenEnd++
				z.optimum[lenEnd].price = kInfinityPrice
			}
			offs := uint32(0)
			for startLen > z.matchDistances[offs] {
				offs += 2
			}

			for lenTest := startLen; ; lenTest++ {
				curBack := z.matchDistances[offs+1]
				curAndLenPrice := normalMatchPrice + z.getPosLenPrice(curBack, lenTest, posState)
				optimum := z.optimum[cur+lenTest]
				if curAndLenPrice < optimum.price {
					optimum.price = curAndLenPrice
					optimum.posPrev = cur
					optimum.backPrev = curBack + kNumRepDistances
					optimum.prev1IsChar = false

					//fmt.Printf("[29] z.getOptimum(): %s\n", optimum.str1())
					//fmt.Printf("[30] z.getOptimum(): %s\n", optimum.str2())
					//fmt.Printf("[31] z.getOptimum(): cur = %d, curBack = %d, curAndLenPrice = %d, lenTest = %d, offs = %d, startLen = %d, "+
					//	"newLen = %d\n",
					//	cur, curBack, curAndLenPrice, lenTest, offs, startLen, newLen)

				}
				if lenTest == z.matchDistances[offs] {
					if lenTest < availableBytesFull {
						t := minUInt32(availableBytesFull-1-lenTest, z.cl.fastBytes)
						lenTest2 := z.mf.iw.getMatchLen(int32(lenTest), curBack, t)
						if lenTest2 >= 2 {
							state2 := stateUpdateMatch(state)
							posStateNext := (position + lenTest) & z.posStateMask
							//getPrice0_temp_F
							curAndLenCharPrice := curAndLenPrice +
								getPrice0(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext])) +
								z.litCoder.getCoder(position+lenTest, z.mf.iw.getIndexByte(int32(lenTest)-1-1)).getPrice(
									true, z.mf.iw.getIndexByte(int32(lenTest)-(int32(curBack)+1)-1),
									z.mf.iw.getIndexByte(int32(lenTest)-1))
								/*
									//======================
									at1 := getPrice0_temp_F(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext]))
									index1 := int32(lenTest) - 1 - 1
									fmt.Println("[31.10] z.getOptimum(): get the subcoder")
									ec2 := z.litCoder.getCoder(position+lenTest, z.mf.iw.getIndexByte(index1))
									fmt.Printf("[31.11] z.getOptimum(): iw.getIndexByte() with arg %d called\n", index1)
									index2 := int32(lenTest) - (int32(curBack) + 1) - 1
									index3 := int32(lenTest) - 1
									at2 := ec2.getPrice(true, z.mf.iw.getIndexByte(index2), z.mf.iw.getIndexByte(index3))
									fmt.Printf("[31.12] z.getOptimum(): iw.getIndexByte() with arg %d called\n", index2)
									fmt.Printf("[31.13] z.getOptimum(): iw.getIndexByte() with arg %d called\n", index3)
									curAndLenCharPrice := curAndLenPrice + at1 + at2
									//======================
								*/
								//fmt.Printf("[31.18] z.getOptimum(): t = %d, availableBytesFull = %d, lenTest = %d, offs = %d, curBack = %d, "+
								//	"lenTest2 = %d, state2 = %d, state = %d\n",
								//	t, availableBytesFull,
								//	lenTest, offs, curBack, lenTest2, state2, state)
								//fmt.Printf("[31.19] z.getOptimum(): posStateNext = %d, position = %d, z.posStateMask = %d, "+
								//	"curAndLenCharPrice = %d, curAndLenPrice = %d\n",
								//	posStateNext, position,
								//	z.posStateMask, curAndLenCharPrice, curAndLenPrice)
								//fmt.Printf("[31.20] z.getOptimum(): index1 = %d, index2 = %d, index3 = %d, at1 = %d, at2 = %d\n",
								//	index1, index2, index3, at1, at2)

							state2 = stateUpdateChar(state2)
							posStateNext = (position + lenTest + 1) & z.posStateMask
							nextMatchPrice := curAndLenCharPrice + getPrice1(uint32(z.isMatch[state2<<kNumPosStatesBitsMax+posStateNext]))
							nextRepMatchPrice := nextMatchPrice + getPrice1(uint32(z.isRep[state2]))
							offset := lenTest + 1 + lenTest2
							for lenEnd < cur+offset {
								lenEnd++
								z.optimum[lenEnd].price = kInfinityPrice
							}
							curAndLenPrice = nextRepMatchPrice + z.getRepPrice(0, lenTest2, state2, posStateNext)
							optimum = z.optimum[cur+offset]
							if curAndLenPrice < optimum.price {
								optimum.price = curAndLenPrice
								optimum.posPrev = cur + lenTest + 1
								optimum.backPrev = 0
								optimum.prev1IsChar = true
								optimum.prev2 = true
								optimum.posPrev2 = cur
								optimum.backPrev2 = curBack + kNumRepDistances

								//fmt.Printf("[32] z.getOptimum(): %s\n", optimum.str1())
								//fmt.Printf("[33] z.getOptimum(): %s\n", optimum.str2())
								//fmt.Printf("[34] z.getOptimum(): cur = %d, curBack = %d, lenTest = %d, curAndLenPrice = %d, "+
								//	"offset = %d, nextRepMatchPrice = %d\n",
								//	cur, curBack, lenTest,
								//	curAndLenPrice, offset, nextRepMatchPrice)
								//fmt.Printf("[35] z.getOptimum(): lenTest2 = %d, state2 = %d, posStateNext = %d, nextMatchPrice = %d, "+
								//	"curAndLenCharPrice = %d, position = %d\n",
								//	lenTest2, state2, posStateNext,
								//	nextMatchPrice, curAndLenCharPrice, position)

							}
						}
					}
					offs += 2

					//fmt.Printf("[35] z.getOptimum(): cur = %d, offs = %d, distancePairs = %d\n", cur, offs, distancePairs)

					if offs == distancePairs {
						break
					}
				}
			}
		}
	}
	return
}

func (z *encoder) fillDistancesPrices() {
	tempPrices := make([]uint32, kNumFullDistances)
	for i := uint32(kStartPosModelIndex); i < kNumFullDistances; i++ {
		posSlot := getPosSlot(i)
		footerBits := posSlot>>1 - 1
		baseVal := (2 | posSlot&1) << footerBits
		tempPrices[i] = reverseGetPriceIndex(z.posCoders, int32(baseVal)-int32(posSlot)-1, footerBits, i-baseVal)
	}
	for lenToPosState := uint32(0); lenToPosState < kNumLenToPosStates; lenToPosState++ {
		var posSlot uint32
		st := lenToPosState << kNumPosSlotBits
		for posSlot = 0; posSlot < z.distTableSize; posSlot++ {
			z.posSlotPrices[st+posSlot] = z.posSlotCoders[lenToPosState].getPrice(posSlot)
			/*
				fmt.Printf("[0] z.fillDistancesPrices(): lenToPosState = %d, posSlot = %d, st = %d, z.posSlotPrices[st+posSlot] = %d\n",
					lenToPosState, posSlot, st, z.posSlotPrices[st+posSlot])
			*/
		}
		for posSlot = kEndPosModelIndex; posSlot < z.distTableSize; posSlot++ {
			z.posSlotPrices[st+posSlot] += (posSlot>>1 - 1 - kNumAlignBits) << kNumBitPriceShiftBits
			/*
				fmt.Printf("[1] z.fillDistancesPrices(): lenToPosState = %d, posSlot = %d, st = %d, z.posSlotPrices[st+posSlot] = %d\n",
					lenToPosState, posSlot, st, z.posSlotPrices[st+posSlot])
			*/
		}
		var i uint32
		st2 := lenToPosState * kNumFullDistances
		for i = 0; i < kStartPosModelIndex; i++ {
			z.distancesPrices[st2+i] = z.posSlotPrices[st+i]
		}
		for ; i < kNumFullDistances; i++ {
			z.distancesPrices[st2+i] = z.posSlotPrices[st+getPosSlot(i)] + tempPrices[i]
		}
	}
	z.matchPriceCount = 0
}

func (z *encoder) fillDistancesPrices_temp_F() {
	tempPrices := make([]uint32, kNumFullDistances)
	for i := uint32(kStartPosModelIndex); i < kNumFullDistances; i++ {
		posSlot := getPosSlot(i)
		footerBits := posSlot>>1 - 1
		baseVal := (2 | posSlot&1) << footerBits
		tempPrices[i] = reverseGetPriceIndex(z.posCoders, int32(baseVal)-int32(posSlot)-1, footerBits, i-baseVal)
	}
	for lenToPosState := uint32(0); lenToPosState < kNumLenToPosStates; lenToPosState++ {
		var posSlot uint32
		st := lenToPosState << kNumPosSlotBits
		for posSlot = 0; posSlot < z.distTableSize; posSlot++ {
			z.posSlotPrices[st+posSlot] = z.posSlotCoders[lenToPosState].getPrice(posSlot)

			//fmt.Printf("[0] z.fillDistancesPrices(): lenToPosState = %d, posSlot = %d, st = %d, z.posSlotPrices[st+posSlot] = %d\n",
			//	lenToPosState, posSlot, st, z.posSlotPrices[st+posSlot])

		}
		for posSlot = kEndPosModelIndex; posSlot < z.distTableSize; posSlot++ {
			z.posSlotPrices[st+posSlot] += (posSlot>>1 - 1 - kNumAlignBits) << kNumBitPriceShiftBits

			//fmt.Printf("[1] z.fillDistancesPrices(): lenToPosState = %d, posSlot = %d, st = %d, z.posSlotPrices[st+posSlot] = %d\n",
			//	lenToPosState, posSlot, st, z.posSlotPrices[st+posSlot])

		}
		var i uint32
		st2 := lenToPosState * kNumFullDistances
		for i = 0; i < kStartPosModelIndex; i++ {
			z.distancesPrices[st2+i] = z.posSlotPrices[st+i]
		}
		for ; i < kNumFullDistances; i++ {
			z.distancesPrices[st2+i] = z.posSlotPrices[st+getPosSlot(i)] + tempPrices[i]
		}
	}
	z.matchPriceCount = 0
}

func (z *encoder) fillAlignPrices() {
	for i := uint32(0); i < kAlignTableSize; i++ {
		z.alignPrices[i] = z.posAlignCoder.reverseGetPrice(i)
	}
	z.alignPriceCount = 0
}

func (z *encoder) writeEndMarker(posState uint32) (err os.Error) {
	if z.writeEndMark != true {
		return
	}
	err = z.re.encode(z.isMatch, z.state<<kNumPosStatesBitsMax+posState, 1)
	if err != nil {
		return
	}
	err = z.re.encode(z.isRep, z.state, 0)
	if err != nil {
		return
	}
	z.state = stateUpdateMatch(z.state)
	length := kMatchMinLen
	err = z.lenCoder.encode(z.re, 0, posState) // 0 is length - kMatchMinLen
	if err != nil {
		return
	}
	posSlot := 1<<kNumPosSlotBits - 1
	lenToPosState := getLenToPosState(uint32(length))
	err = z.posSlotCoders[lenToPosState].encode(z.re, uint32(posSlot))
	if err != nil {
		return
	}
	footerBits := uint32(30)
	posReduced := 1<<footerBits - 1
	err = z.re.encodeDirectBits(int32(posReduced>>kNumAlignBits), int32(footerBits)-kNumAlignBits)
	if err != nil {
		return
	}
	err = z.posAlignCoder.reverseEncode(z.re, uint32(posReduced&kAlignMask))
	if err != nil {
		return
	}
	return
}

func (z *encoder) flush(nowPos uint32) (err os.Error) {
	err = z.writeEndMarker(uint32(nowPos) & z.posStateMask)
	if err != nil {
		return
	}
	err = z.re.flushData()
	if err != nil {
		return
	}
	err = z.re.flushStream()
	return
}

func (z *encoder) codeOneBlock() (err os.Error) {
	z.finished = true
	progressPosValuePrev := z.nowPos
	if z.nowPos == 0 {
		if z.mf.iw.getNumAvailableBytes() == 0 {
			err = z.flush(uint32(z.nowPos))
			return
		}
		_, err = z.readMatchDistances()
		if err != nil {
			return
		}
		err = z.re.encode(z.isMatch, uint32(z.state<<kNumPosStatesBitsMax)+uint32(z.nowPos)&z.posStateMask, 0)
		if err != nil {
			return
		}
		z.state = stateUpdateChar(z.state)
		// TODO: z.mf.iw.blabla is too long ...
		curByte := z.mf.iw.getIndexByte(0 - int32(z.additionalOffset))
		//fmt.Printf("[0.10] z.copyOneBlock(): iw.getIndexByte() with arg %d called\n", 0-int32(z.additionalOffset))
		err = z.litCoder.getCoder(uint32(z.nowPos), z.prevByte).encode(z.re, curByte)
		if err != nil {
			return
		}
		z.prevByte = curByte
		z.additionalOffset--
		z.nowPos++
	}
	if z.mf.iw.getNumAvailableBytes() == 0 {
		err = z.flush(uint32(z.nowPos))
		return
	}
	for {
		length, err := z.getOptimum(uint32(z.nowPos))
		if err != nil {
			return
		}
		pos := z.backRes
		posState := uint32(z.nowPos) & z.posStateMask
		complexState := z.state<<kNumPosStatesBitsMax + posState

		//fmt.Printf("[1] z.codeOnBlock(): progressPosValuePrev = %d, z.prevByte = %d, z.additionalOffset = %d\n",
		//	progressPosValuePrev, int8(z.prevByte), z.additionalOffset)
		//fmt.Printf("[2] z.codeOnBlock(): z.nowPos = %d, length = %d, pos = %d, posState = %d, complexState = %d\n",
		//	z.nowPos, length, int32(pos), posState, complexState)

		if length == 1 && pos == 0xFFFFFFFF {
			err = z.re.encode(z.isMatch, complexState, 0)
			if err != nil {
				return
			}
			curByte := z.mf.iw.getIndexByte(0 - int32(z.additionalOffset))
			//fmt.Printf("[2.11] z.copyOneBlock(): iw.getIndexByte() with arg %d called\n", 0-int32(z.additionalOffset))
			lc2 := z.litCoder.getCoder(uint32(z.nowPos), z.prevByte)
			if stateIsCharState(z.state) == false {
				matchByte := z.mf.iw.getIndexByte(0 - int32(z.repDistances[0]) - 1 - int32(z.additionalOffset))
				//fmt.Printf("[2.14] z.copyOneBlock(): iw.getIndexByte() with arg %d called\n", 0-int32(z.repDistances[0])-1-int32(z.additionalOffset))
				err = lc2.encodeMatched(z.re, matchByte, curByte)
				if err != nil {
					return
				}
			} else {
				err = lc2.encode(z.re, curByte)
				if err != nil {
					return
				}
			}
			z.prevByte = curByte
			z.state = stateUpdateChar(z.state)
		} else {
			err = z.re.encode(z.isMatch, complexState, 1)
			if err != nil {
				return
			}
			if pos < kNumRepDistances {
				err = z.re.encode(z.isRep, z.state, 1)
				if err != nil {
					return
				}
				if pos == 0 {
					err = z.re.encode(z.isRepG0, z.state, 0)
					if err != nil {
						return
					}
					if length == 1 {
						err = z.re.encode(z.isRep0Long, complexState, 0)
						if err != nil {
							return
						}
					} else {
						err = z.re.encode(z.isRep0Long, complexState, 1)
						if err != nil {
							return
						}
					}
				} else {
					err = z.re.encode(z.isRepG0, z.state, 1)
					if err != nil {
						return
					}
					if pos == 1 {
						err = z.re.encode(z.isRepG1, z.state, 0)
						if err != nil {
							return
						}
					} else {
						err = z.re.encode(z.isRepG1, z.state, 1)
						if err != nil {
							return
						}
						err = z.re.encode(z.isRepG2, z.state, uint32(pos-2))
						if err != nil {
							return
						}
					}
				}
				if length == 1 {
					z.state = stateUpdateShortRep(z.state)
				} else {
					err = z.repMatchLenCoder.encode(z.re, uint32(length-kMatchMinLen), posState)
					if err != nil {
						return
					}
					z.state = stateUpdateRep(z.state)
				}
				distance := z.repDistances[pos]
				if pos != 0 {
					for i := pos; i >= 1; i-- {
						z.repDistances[i] = z.repDistances[i-1]
					}
					z.repDistances[0] = distance
				}
			} else {
				err = z.re.encode(z.isRep, z.state, 0)
				if err != nil {
					return
				}
				z.state = stateUpdateMatch(z.state)
				err = z.lenCoder.encode(z.re, uint32(length-kMatchMinLen), posState)
				if err != nil {
					return
				}
				pos -= kNumRepDistances
				posSlot := getPosSlot(uint32(pos))
				lenToPosState := getLenToPosState(uint32(length))
				err = z.posSlotCoders[lenToPosState].encode(z.re, posSlot)

				//fmt.Printf("[2.18] z.copyOneBlock(): posSlot = %d, length = %d, pos = %d, lenToPosState = %d, z.state = %d\n",
				//	posSlot, length, pos, lenToPosState, z.state)

				if err != nil {
					return
				}
				if posSlot >= kStartPosModelIndex {
					footerBits := posSlot>>1 - 1
					baseVal := (2 | posSlot&1) << footerBits
					posReduced := pos - baseVal
					if posSlot < kEndPosModelIndex {
						err = reverseEncodeIndex(z.re, z.posCoders, int32(baseVal)-int32(posSlot)-1, footerBits, uint32(posReduced))
						if err != nil {
							return
						}
					} else {
						err = z.re.encodeDirectBits(int32(posReduced)>>kNumAlignBits, int32(footerBits)-kNumAlignBits)
						if err != nil {
							return
						}
						err = z.posAlignCoder.reverseEncode(z.re, uint32(posReduced&kAlignMask))
						if err != nil {
							return
						}
						z.alignPriceCount++
					}
				}
				for i := kNumRepDistances - 1; i >= 1; i-- {
					z.repDistances[i] = z.repDistances[i-1]
				}
				z.repDistances[0] = pos
				z.matchPriceCount++
			}
			z.prevByte = z.mf.iw.getIndexByte(int32(length) - 1 - int32(z.additionalOffset))
			//fmt.Printf("[2.25] z.copyOneBlock(): iw.getIndexByte() with arg %d called\n", int32(length)-1-int32(z.additionalOffset))
		}
		z.additionalOffset -= length
		z.nowPos += int64(length)

		//fmt.Printf("[3] z.copyOneBlock(): z.additionalOffset = %d, z.nowPos = %d, length = %d\n", z.additionalOffset, z.nowPos, length)

		if z.additionalOffset == 0 {

			//fmt.Printf("[4] z.copyOneBlock(): z.matchPriceCount = %d, z.alignPriceCount = %d, z.mf.iw.getNumAvailableBytes() = %d\n",
			//	z.matchPriceCount, z.alignPriceCount, z.mf.iw.getNumAvailableBytes())

			if z.matchPriceCount >= 1<<7 {
				z.fillDistancesPrices_temp_F()
			}
			if z.alignPriceCount >= kAlignTableSize {
				z.fillAlignPrices()
			}
			if z.mf.iw.getNumAvailableBytes() == 0 {
				err = z.flush(uint32(z.nowPos))
				return
			}
			if z.nowPos-progressPosValuePrev >= 1<<12 {
				z.finished = false
				return
			}
		}
	}
	return
}

func (z *encoder) doEncode() (err os.Error) {
	for {
		err = z.codeOneBlock()
		if err != nil {
			return
		}
		if z.finished == true {
			//println("z.finished is true")
			break
		}
	}
	return
}

func (z *encoder) encoder(r io.Reader, w io.Writer, size int64, level int) (err os.Error) {

	initProbPrices()
	initCrcTable()
	initGFastPos()

	if level < 1 || level > 9 {
		return os.NewError("level out of range: " + string(level))
	}
	z.cl = levels[level]
	err = z.cl.checkValues()
	if err != nil {
		return
	}
	z.distTableSize = z.cl.dictSize * 2
	z.cl.dictSize = 1 << z.cl.dictSize
	if size < -1 { // -1 stands for unknown size, but can the size be equal to zero ?
		return os.NewError("illegal size: " + string(size))
	}
	z.size = size
	z.writeEndMark = false
	if z.size == -1 {
		z.writeEndMark = true
	}

	header := make([]byte, lzmaHeaderSize)
	header[0] = byte((z.cl.posStateBits*5+z.cl.litPosStateBits)*9 + z.cl.litContextBits)
	for i := uint32(0); i < 4; i++ {
		header[i+1] = byte(z.cl.dictSize >> (8 * i))
	}
	for i := uint32(0); i < 8; i++ {
		header[i+lzmaPropSize] = byte(uint64(z.size>>(8*i)) & 0xFF)
	}
	n, err := w.Write(header)
	if err != nil {
		return
	}
	if n != len(header) {
		return os.NewError("error writing lzma header")
	}

	z.re = newRangeEncoder(w)
	mft, err := strconv.Atoui(strings.Split(z.cl.matchFinder, "", 0)[2])
	z.matchFinderType = uint32(mft)
	if err != nil {
		return
	}
	numHashBytes := uint32(4)
	if z.matchFinderType == eMatchFinderTypeBT2 {
		numHashBytes = 2
	}
	z.mf, err = newLzBinTree(r, z.cl.dictSize, kNumOpts, z.cl.fastBytes, kMatchMaxLen+1, numHashBytes)
	if err != nil {
		return
	}

	z.optimum = make([]*optimal, kNumOpts)
	for i := 0; i < kNumOpts; i++ {
		z.optimum[i] = &optimal{}
	}
	z.isMatch = initBitModels(kNumStates << kNumPosStatesBitsMax)
	z.isRep = initBitModels(kNumStates)
	z.isRepG0 = initBitModels(kNumStates)
	z.isRepG1 = initBitModels(kNumStates)
	z.isRepG2 = initBitModels(kNumStates)
	z.isRep0Long = initBitModels(kNumStates << kNumPosStatesBitsMax)
	z.posSlotCoders = make([]*rangeBitTreeCoder, kNumLenToPosStates)
	for i := 0; i < kNumLenToPosStates; i++ {
		z.posSlotCoders[i] = newRangeBitTreeCoder(kNumPosSlotBits)
	}
	z.posCoders = initBitModels(kNumFullDistances - kEndPosModelIndex)
	z.posAlignCoder = newRangeBitTreeCoder(kNumAlignBits)
	z.lenCoder = newLenPriceTableCoder(z.cl.fastBytes+1-kMatchMinLen, 1<<z.cl.posStateBits)
	z.repMatchLenCoder = newLenPriceTableCoder(z.cl.fastBytes+1-kMatchMinLen, 1<<z.cl.posStateBits)
	z.litCoder = newLitCoder(z.cl.litPosStateBits, z.cl.litContextBits)
	z.matchDistances = make([]uint32, kMatchMaxLen*2+2)
	z.additionalOffset = 0
	z.optimumEndIndex = 0
	z.optimumCurrentIndex = 0
	z.longestMatchFound = false
	z.posSlotPrices = make([]uint32, 1<<(kNumPosSlotBits+kNumLenToPosStatesBits))
	z.distancesPrices = make([]uint32, kNumFullDistances<<kNumLenToPosStatesBits)
	z.alignPrices = make([]uint32, kAlignTableSize)
	z.posStateMask = 1<<z.cl.posStateBits - 1
	z.nowPos = 0
	z.finished = false

	z.state = 0
	z.prevByte = 0
	z.repDistances = make([]uint32, kNumRepDistances)
	for i := 0; i < kNumRepDistances; i++ {
		z.repDistances[i] = 0
	}

	z.matchPriceCount = 0

	z.reps = make([]uint32, kNumRepDistances)
	z.repLens = make([]uint32, kNumRepDistances)

	z.fillDistancesPrices()
	z.fillAlignPrices()

	err = z.doEncode()
	return
}

// This contructor shall be used when a custom level of compression is nedded
// and the size of uncompressed data is known. Unlike gzip which stores the
// size and the chechsum of uncompressed data at the end of the compressed file,
// lzma stores this information at the begining. For this reason lzma can't
// compute the size. Therefore the user must pass the size of the file written
// to w, or choose another contructor which uses -1 for the size and writes a
// marker of 6 bytes at the end of the stream.
//
func NewEncoderFileLevel(w io.Writer, size int64, level int) io.WriteCloser {
	var z encoder
	pr, pw := syncPipe()
	//pr, pw := io.Pipe()
	go func() {
		err := z.encoder(pr, w, size, level)
		pr.CloseWithError(err)
	}()
	return pw
}

// This contructor shall be used when a custom level of compression is nedded,
// but the size of uncompressed data is unknown. Same as
// NewEncoderFileLevel(w, -1, level).
//
func NewEncoderStreamLevel(w io.Writer, level int) io.WriteCloser {
	return NewEncoderFileLevel(w, -1, level)
}

// This contructor shall be used when size of uncompressed data in known. Same
// as NewEncoderFileLevel(w, size, DefaultCompression).
//
func NewEncoderFile(w io.Writer, size int64) io.WriteCloser {
	return NewEncoderFileLevel(w, size, DefaultCompression)
}

// This contructor shall be used when the size of uncompressed data is unknown.
// Reading the whole stream into memoty to find out it's size is not an option.
// Same as NewEncoderFileLevel(w, -1, DefaultCompression).
//
func NewEncoderStream(w io.Writer) io.WriteCloser {
	return NewEncoderStreamLevel(w, DefaultCompression)
}
