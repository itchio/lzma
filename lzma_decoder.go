/*
The lzma package implements reading and writing of LZMA
format compressed data originaly developed by Igor Pavlov.
As reference implementations have been taken LZMA SDK version 4.65
that can be found at:

  http://www.7-zip.org/sdk.html

Note that LZMA doesn't store any metadata about the file. Neither can
it compress multiple files because it's not an archiving format. Both
these issues are solved if the file or files are archived with tar
before compression with LZMA.

LZMA compressed file format
---------------------------
Offset Size Description
  0     1   Special LZMA properties (lc,lp, pb in encoded form)
  1     4   Dictionary size (little endian)
  5     8   Uncompressed size (little endian). -1 means unknown size
 13         Compressed data

The implementation provides filters that uncompress during reading
and compress during writing.  For example, to write compressed data
to a buffer:

        var b bytes.Buffer
        w, err := lzma.NewEncoder(&b)
        w.Write([]byte("hello, world\n"))
        w.Close()

and to read that data back:

        r, err := lzma.NewDecoder(&b)
        io.Copy(os.Stdout, r)
        r.Close()
*/
package lzma

import (
	"io"
	"os"
)

const (
	inBufSize           = 1 << 16
	outBufSize          = 1 << 16
	lzmaPropSize        = 5
	lzmaHeaderSize      = lzmaPropSize + 8
	lzmaMaxReqInputSize = 20
)

// lzma pproperties
type props struct {
	lc, lp, pb uint8
	dicSize    uint32
}

type decoder struct { // flate.inflater, zlib.reader, gzip.inflater
	// input sources
	r rangeDecoder
	w io.Writer

	// lzma header
	prop       props
	unpackSize int64

/*	// hz
	probs  *uint16
	dic    *byte
	buf    *byte
	rrange uint32
	code   uint32
	dicPos uint32
	// dicBufSize == prop.dicSize
	processedPos  uint32
	chechDicSize  uint32
	state         uint
	needFlush     int
	needInitState int
	numProbs      uint32
	tempBufSize   uint
	tempBuf       [lzmaMaxReqInputSize]byte*/

	eos bool
	err os.Error
}

func (z *decoder) doDecode() (err os.Error) {

	return nil
}

func (z *decoder) decodeProps(buf []byte) (err os.Error) {
	d := buf[0]
	if d > (9 * 5 * 5) {
		return os.NewError("illegal value of encoded lc, lp, pb byte")
	}
	z.prop.lc = d % 9
	d /= 9
	z.prop.pb = d / 5
	z.prop.lp = d % 5
	z.prop.dicSize = uint32(buf[1]) | uint32(buf[2]<<8) | uint32(buf[3]<<16) | uint32(buf[4]<<24)
	return
}

// decoder initializes a decoder; it reads first 13 bytes from r which contain
// lc, lp, pb, dicSize and unpackedSize; next creates a rangeDecoder; the
// rangeDecoder should be created after lzmaHeader is read from r because
// newRangeDecoder() further reads from the same stream 5 bytes to 
// init rangeDecoder.code
func (z *decoder) decoder(r io.Reader, w io.Writer) (err os.Error) {
	z.w = w
	header := make([]byte, lzmaHeaderSize)
	n, err := r.Read(header)
	if n != lzmaHeaderSize {
		return os.NewError("read " + string(n) + " bytes instead of " + string(lzmaHeaderSize))
	}
	if err != nil {
		return
	}
	if err := z.decodeProps(header); err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		z.unpackSize += int64(header[lzmaPropSize+i] << uint8(8*i))
	}
	if z.unpackSize == -1 {
		z.eos = true
	}
	if err := z.doDecode(); err != nil {
		return
	}
	z.r, err = newRangeDecoder(r)
	if err != nil {
		return
	}
	return
}

func NewDecoder(r io.Reader) (io.ReadCloser, os.Error) {
	var z decoder
	pr, pw := io.Pipe()
	go func() {
		err := z.decoder(r, pw)
		pw.CloseWithError(err)
	}()
	return pr, nil
}
