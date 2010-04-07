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

type compressionLevel struct {
	dictSize           int    // dictionary size, computed as (1 << D)
	fastBytes          int    // number of fast bytes
	literalContextBits int    // number of literal context bits
	literalPosBits     int    // number of literal pos bits
	posBits            int    // number of pos bits
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

type InternalError string


// size
// eos

type encoder struct { // flate.deflater, zlib.writer, gzip.deflater
	compressionLevel
	encoder io.WriteCloser
	err     os.Error
}

func NewEncoder(w io.Writer) (io.WriteCloser, os.Error) {
	return NewEncoderLevel(w, DefaultCompression)
}

func NewEncoderLevel(w io.Writer, level int) (io.WriteCloser, os.Error) {
	if level < 0 || level > 9 {
		return nil, os.NewError("level out of range")
	}
	return newEncoderCompressionLevel(w, levels[level])
}

func newEncoderCompressionLevel(w io.Writer, cl compressionLevel) (io.WriteCloser, os.Error) {
	// do smth usefull
	return nil, nil // TODO rc and nil
}

func (z *encoder) Write(p []byte) (n int, err os.Error) {
	return
}

func (z *encoder) Close() os.Error {
	return nil
}
