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
	//compressionMode    uint32 // compression mode // a
	dictSize  uint32 // dictionary size, computed as (1 << d) // d
	fastBytes uint32 // number of fast bytes // fb
	//matchCycles        uint32 // number of cycles for match finder // mc
	litContextBits uint32 // number of literal context bits // lc
	litPosBits     uint32 // number of literal pos bits // lp
	posBits        uint32 // number of pos bits // pb
	matchFinder    string // match finder // mf
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
	// max 1 << 29 bytes in Java version
	// max 1 << 30 bytes ANSI C and CPP versions
	if cl.dictSize < 12 || cl.dictSize > 30 {
		return os.NewError("dictionary size out of range: " + string(cl.dictSize))
	}
	if cl.fastBytes < 5 || cl.fastBytes > 273 {
		return os.NewError("number of fast bytes out of range: " + string(cl.fastBytes))
	}
	if cl.litContextBits < 0 || cl.litContextBits > 8 {
		return os.NewError("number of literal context bits out of range: " + string(cl.litContextBits))
	}
	if cl.litPosBits < 0 || cl.litPosBits > 4 {
		return os.NewError("number of literal position bits out of range: " + string(cl.litPosBits))
	}
	if cl.posBits < 0 || cl.posBits > 4 {
		return os.NewError("number of position bits out of range: " + string(cl.posBits))
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
	backs3 int32

	prev1IsChar,
	prev2 bool
}

func (o *optimal) makeAsChar() {
	o.backPrev = -1
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
	eMatchFinderTypeBT2  uint32 = 0
	eMatchFinderTypeBT4  uint32 = 1
	kIfinityPrice        int32  = 0xFFFFFFF
	kDefaultDicLogSize   int32  = 22
	kNumFastBytesDefault int32  = 0x20
	kNumLenSpecSymbols   = kNumLowLenSymbols + kNumMidLenSymbols
	kNumOpts             = 1 << 12
)

type encoder struct {
	// i/o
	re          *rangeEncoder // w
	matchFinder *lzBinTree    // r

	cl           *compressionLevel
	size         int64
	writeEndMark bool // eos

	optimum             []*optimal
	isMatch             []uint16
	isRep               []uint16
	isRepG0             []uint16
	isRepG1             []uint16
	isRepG2             []uint16
	isRep0Long          []uint16
	posSlotCoders       []*rangeBitTreeCoder
	posEncoders         []uint16
	posAlignCoder       *rangeBitTreeCoder
	lenCoder            *lenPriceTableCoder
	repLenCoder         *lenPriceTableCoder
	litCoder            *litCoder
	matchDistances      []uint32
	longestMatchLen     uint32
	distancePairs       int32
	additionalOffset    int32
	optimumEndIndex     int32
	optimumCurrentIndex int32
	longestMatchFound   bool
	posSlotPrices       []uint32
	distancesPrices     []uint32
	alignPrices         []uint32
	alignPriceCount     uint32
	distTableSize       uint32
	posStateMask        uint32
	//dictSizePrev        int32
	//fastBytesPres       int32
	nowPos          int64
	finished        bool
	matchFinderType uint32
	//needReleaseMFStream bool
	state           int32
	prevByte        byte
	repDistances    []int32
	matchPriceCount uint32

	//posStateBits uint32 // posBits ?
	//fastBytes uint32
	//litPosStateBits uint32
	//litContextBits uint32
	//dictSize uint32
	//matchFinder string
}

func (z *encoder) doEncode() (err os.Error) {
	return
}

func (z *encoder) fillDistancesPrices() {
	tempPrices := make([]uint32, kNumFullDistances)
	for i := uint32(kStartPosModelIndex); i < kNumFullDistances; i++ {
		posSlot := getPosSlot(i)
		footerBits := posSlot>>1 - 1
		baseVal := (2 | posSlot&1) << footerBits
		tempPrices[i] = reverseGetPriceIndex(z.posEncoders, int32(baseVal)-int32(posSlot)-1, footerBits, i-baseVal)
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
	header[0] = byte((z.cl.posBits*5+z.cl.litPosBits)*9 + z.cl.litContextBits)
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
	numHashBytes := int32(4)
	if z.matchFinderType == eMatchFinderTypeBT2 {
		numHashBytes = 2
	}
	z.matchFinder, err = newLzBinTree(r, int32(z.cl.dictSize), kNumOpts, int32(z.cl.fastBytes), kMatchMaxLen+1, numHashBytes)
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
	z.posEncoders = make([]uint16, kNumFullDistances-kEndPosModelIndex)
	z.posAlignCoder = newRangeBitTreeCoder(kNumAlignBits)
	z.lenCoder = newLenPriceTableCoder(z.cl.fastBytes+1-kMatchMinLen, 1<<z.cl.posBits)
	z.repLenCoder = newLenPriceTableCoder(z.cl.fastBytes+1-kMatchMinLen, 1<<z.cl.posBits)
	z.litCoder = newLitCoder(z.cl.litPosBits, z.cl.litContextBits)
	z.matchDistances = make([]uint32, kMatchMaxLen*2+2)
	//z.longestMatchLen = uninitialized
	//z.distancePairs = unitialized
	z.additionalOffset = 0
	z.optimumEndIndex = 0
	z.optimumCurrentIndex = 0
	z.longestMatchFound = false
	z.posSlotPrices = make([]uint32, 1<<(kNumPosSlotBits+kNumLenToPosStatesBits))
	z.distancesPrices = make([]uint32, kNumFullDistances<<kNumLenToPosStatesBits)
	z.alignPrices = make([]uint32, kAlignTableSize)
	//for i := uint32(0); i < kAlignTableSize; i++ {
	//	z.alignPrices[i] = z.posAlignCoder.reverseGetPrice(i)
	//}
	//z.alignPriceCount = 0
	//z.distTableSize = kDefaultDictionaryLogSize * 2
	z.posStateMask = 1<<z.cl.posBits - 1
	//z.dictSizePrev = -1
	//z.fastBytesPrev = -1
	z.nowPos = 0
	z.finished = false
	//z.needReleaseMFStream = hz

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
