package lzma

import (
	"io"
	"os"
)

const (
	BestSpeed          = 1
	BestCompression    = 9
	DefaultCompression = 7
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
	dictSize           uint32 // dictionary size, computed as (1 << D)
	fastBytes          uint32 // number of fast bytes
	literalContextBits uint32 // number of literal context bits
	literalPosBits     uint32 // number of literal pos bits
	posBits            uint32 // number of pos bits
	matchFinder        string // Match Finder
}

var levels = []compressionLevel{
	compressionLevel{},                        // 0
	compressionLevel{16, 64, 3, 0, 2, "bt4"},  // 1
	compressionLevel{18, 64, 3, 0, 2, "bt4"},  // 2
	compressionLevel{19, 64, 3, 0, 2, "bt4"},  // 3
	compressionLevel{20, 64, 3, 0, 2, "bt4"},  // 4
	compressionLevel{21, 128, 3, 0, 2, "bt4"}, // 5
	compressionLevel{22, 128, 3, 0, 2, "bt4"}, // 6
	compressionLevel{23, 128, 3, 0, 2, "bt4"}, // 7
	compressionLevel{24, 255, 3, 0, 2, "bt4"}, // 8
	compressionLevel{25, 255, 3, 0, 2, "bt4"}, // 9
}

func (cl compressionLevel) checkValues() os.Error {
	// (1 << 29) bytes or 512 MiB in Java version
	// (1 << 30) bytes or 1 GiB in C version
	// (1 << 32) bytes or 4 GiB theoretical maximum
	if cl.dictSize < 0 || cl.dictSize > 29 {
		return os.NewError("dictionary size out of range: " + string(cl.dictSize))
	}
	// TODO(eu): replace magic numbers with constants
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
	if cl.matchFinder != "bt2" || cl.matchFinder != "bt4" {
		return os.NewError("unsuported match finder: " + cl.matchFinder)
	}
	return nil
}

type encoder struct { // flate.deflater, zlib.writer, gzip.deflater
	compressionLevel
	w	io.Writer
	r	io.Reader
	size	uint64
	eos	bool
	err	os.Error
}

func (z *encoder) encode(r io.Reader, w io.Writer, cl compressionLevel) (err os.Error) {
	return nil
}

func newEncoderCompressionLevel(w io.Writer, cl compressionLevel) (io.WriteCloser, os.Error) {
        if err := cl.checkValues(); err != nil {
                return nil, err
        }
        cl.dictSize = 1 << cl.dictSize

	var z encoder
	pr, pw := syncPipe()
	go func() {
		err := z.encode(pr, w, cl)
		pr.CloseWithError(err)
	}()

        return pw, nil
}

func NewEncoderLevel(w io.Writer, level int) (io.WriteCloser, os.Error) {
        if level < 0 || level > 9 {
                return nil, os.NewError("level out of range")
        }
        return newEncoderCompressionLevel(w, levels[level])
}

func NewEncoder(w io.Writer) (io.WriteCloser, os.Error) {
        return NewEncoderLevel(w, DefaultCompression)
}
