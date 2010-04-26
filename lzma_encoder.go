package lzma

import (
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	BestSpeed          = 1
	BestCompression    = 9
	DefaultCompression = 6
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
	if cl.matchFinder != "bt2" || cl.matchFinder != "bt4" { // there are also bt3 and hc4, but will implement them later
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
		return uint32(gFastPos[pos>>6] + 16)
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


const (
	eMatchFinderTypeBT2  = 0
	eMatchFinderTypeBT4  = 1
	kIfinityPrice        = 0xFFFFFFF
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
	return
}

// singature: c | go | cs
func (z *encoder) getPosLenPrice(pos, length, posState uint32) (price uint32) {
	lenToPosState := getLenToPosState(length)
	if pos < kNumFullDistances {
		price = z.distancesPrices[lenToPosState*kNumFullDistances+pos]
	} else {
		price = z.posSlotPrices[lenToPosState*kNumPosSlotBits+getPosSlot2(pos)] + z.alignPrices[pos&kAlignMask]
	}
	price += z.lenCoder.getPrice(length-kMatchMinLen, posState)
	return
}

// signature: c | go | cs
func (z *encoder) getPosLen1Price(state, posState uint32) uint32 {
	return getPrice0(uint32(z.isRepG0[state])) +
		getPrice0(uint32(z.isRep0Long[state<<kNumPosStatesBitsMax+posState]))
}

// signature: c | go | cs
func (z *encoder) backward(cur uint32) uint32 {
	z.optimumEndIndex = cur
	posMem := z.optimum[cur].posPrev
	backMem := z.optimum[cur].backPrev
	tmp := uint32(1) // execute loop at least once (do-while)
	for ; tmp > 0; tmp = cur {
		if z.optimum[cur].prev1IsChar == true {
			z.optimum[posMem].makeAsChar()
			z.optimum[posMem].posPrev = posMem - 1
			if z.optimum[cur].prev2 == true {
				z.optimum[posMem-1].prev1IsChar = false
				z.optimum[posMem-1].posPrev = z.optimum[cur].posPrev2
				z.optimum[posMem-1].backPrev = z.optimum[cur].backPrev2
			}
		}
		posPrev := posMem
		backCur := backMem
		backMem = z.optimum[posPrev].backPrev
		posMem = z.optimum[posPrev].posPrev
		z.optimum[posPrev].backPrev = backCur
		z.optimum[posPrev].posPrev = cur
		cur = posPrev
	}
	z.backRes = z.optimum[0].backPrev
	z.optimumCurrentIndex = z.optimum[0].posPrev
	return z.optimumCurrentIndex
}

func (z *encoder) getOptimum(nowPos uint32) uint32 {
	// TODO: code it
	return 0
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
		}
		for posSlot = kEndPosModelIndex; posSlot < z.distTableSize; posSlot++ {
			z.posSlotPrices[st+posSlot] += (posSlot>>1 - 1 - kNumAlignBits) << kNumBitPriceShiftBits
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
		length := z.getOptimum(uint32(z.nowPos))
		pos := z.backRes
		posState := uint32(z.nowPos) & z.posStateMask
		complexState := z.state<<kNumPosStatesBitsMax + posState
		if length == 1 && pos == 0xFFFFFFFF {
			err = z.re.encode(z.isMatch, complexState, 0)
			if err != nil {
				return
			}
			curByte := z.mf.iw.getIndexByte(0 - int32(z.additionalOffset))
			lc2 := z.litCoder.getCoder(uint32(z.nowPos), z.prevByte)
			if stateIsCharState(z.state) == false {
				matchByte := z.mf.iw.getIndexByte(0 - int32(z.repDistances[0]) - 1 - int32(z.additionalOffset))
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
		}
		z.additionalOffset -= length
		z.nowPos += int64(length)
		if z.additionalOffset == 0 {
			if z.matchPriceCount >= 1<<7 {
				z.fillDistancesPrices()
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
			break
		}
	}
	return
}

func (z *encoder) encoder(r io.Reader, w io.Writer, size int64, level int) (err os.Error) {
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
	z.posCoders = make([]uint16, kNumFullDistances-kEndPosModelIndex)
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
	for i := 0; i < kNumRepDistances; i++ {
		z.repDistances[i] = 0
	}

	z.matchPriceCount = 0

	initProbPrices()
	initCrcTable()
	initGFastPos()
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
