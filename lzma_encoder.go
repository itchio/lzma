package lzma

import (
	"io"
	"os"
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
	literalContextBits uint32 // number of literal context bits // lc
	literalPosBits     uint32 // number of literal pos bits // lp
	posBits            uint32 // number of pos bits // pb
	matchFinder        string // match finder // mf
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
	// 1 << 29 bytes or 512 MiB in Java version
	// 1 << 30 bytes or 1 GiB in ANSI C version
	// 1 << 32 bytes or 4 GiB theoretical maximum
	if cl.dictSize < 12 || cl.dictSize > 29 {
		return os.NewError("dictionary size out of range: " + string(cl.dictSize))
	}
	if cl.fastBytes < 5 || cl.fastBytes > 273 {
		return os.NewError("number of fast bytes out of range: " + string(cl.fastBytes))
	}
	if cl.literalContextBits < 0 || cl.literalContextBits > 8 {
		return os.NewError("number of literal context bits out of range: " + string(cl.literalContextBits))
	}
	if cl.literalPosBits < 0 || cl.literalPosBits > 4 {
		return os.NewError("number of literal position bits out of range: " + string(cl.literalPosBits))
	}
	if cl.posBits < 0 || cl.posBits > 4 {
		return os.NewError("number of position bits out of range: " + string(cl.posBits))
	}
	if cl.matchFinder != "bt2" || cl.matchFinder != "bt4" { // there are also bt3 and hc4, but will implrement them later
		return os.NewError("unsuported match finder: " + cl.matchFinder)
	}
	return nil
}


const (
	eMatchFinderTypeBT2  int32 = 0
	eMatchFinderTypeBT4  int32 = 1
	kIfinityPrice        int32 = 0xFFFFFFF
	kDefaultDicLogSize   int32 = 22
	kNumFastBytesDefault int32 = 0x20
	kNumLenSpecSymbols   = kNumLowLenSymbols + kNumMidLenSymbols
	kNumOpts             = 1 << 12
)

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

func getPosSlot(pos int32) int32 {
	if pos < 1<<11 {
		return int32(gFastPos[pos])
	}
	if pos < 1<<21 {
		return int32(gFastPos[pos>>10] + 20)
	}
	return int32(gFastPos[pos>>20] + 40)
}

func getPosSlot2(pos int32) int32 {
	if pos < 1<<17 {
		return int32(gFastPos[pos>>6] + 16)
	}
	if pos < 1<<27 {
		return int32(gFastPos[pos>>16] + 32)
	}
	return int32(gFastPos[pos>>26] + 52)
}


type encoder struct {
	w    io.Writer
	r    io.Reader
	cl   *compressionLevel
	size int64
	eos  bool

	state        int32
	prevByte     byte
	repDistances []int32
}

func (z *encoder) doEncode() (err os.Error) {
	return
}

func (z *encoder) encoder(r io.Reader, w io.Writer, size int64, level int) (err os.Error) {
	z.w = w
	z.r = r

	if level < 1 || level > 9 {
		return os.NewError("level out of range: " + string(level))
	}
	z.cl = levels[level]
	err = z.cl.checkValues()
	if err != nil {
		return
	}
	z.cl.dictSize = 1 << z.cl.dictSize

	if size < -1 { // -1 stands for unknown size, but can the size be equal to zero ?
		return os.NewError("illegal size: " + string(size))
	}
	z.size = size

	z.eos = false
	if z.size == -1 {
		z.eos = true
	}

	z.state = 0
	z.prevByte = 0
	for i := 0; i < kNumRepDistances; i++ {
		z.repDistances[i] = 0
	}

	initProbPrices()
	initCrcTable()
	initGFastPos()

	err = z.doEncode()
	return
}

func newEncoderCompressionLevel(w io.Writer, size int64, level int) io.WriteCloser {
	var z encoder
	pr, pw := syncPipe()
	go func() {
		err := z.encoder(pr, w, size, level)
		pr.CloseWithError(err)
	}()
	return pw
}

// This contructor shall be used when a custom level of compression is nedded
// and the size of uncompressed data is known. Unlike gzip which stores the
// size and the chechsum of uncompressed data at the end of the compressed file,
// lzma stores this information at the begining. For this reason the caller must
// pass the size of the file written to w, or choose a *Stream contructor which
// uses -1 for the size and a marker of 6 bytes at the end of the stream.
//
func NewEncoderFileLevel(w io.Writer, size int64, level int) io.WriteCloser {
	return newEncoderCompressionLevel(w, size, level)
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
