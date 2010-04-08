/*
The lzma package implements reading and writing of LZMA
format compressed data originaly developed by Igor Pavlov.
As reference implementations have been taken LZMA SDK version 4.65
that can be found at:

  http://www.7-zip.org/sdk.html

Note that LZMA doesn't store any metadata about the file. Neither can
it compress multiple files because it's not an archiving format. Both
these issues are solved if the file or files are wrapped into a tar
archive before compression with LZMA.

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

TODO:
  archive/7z
  compress/bzip2
  compress/lzma2	// LZMA SDK version 9.xx (9.12 at the time of writing this) is still beta
  compress/ppmd

*/
package lzma

import (
	"io"
	"os"
)

type decoder struct { // flate.inflater, zlib.reader, gzip.inflater
	cl   compressionLevel
	r    io.Reader
	w    io.Writer
	size uint64
	eos  bool
	err  os.Error
}

func (z *decoder) decoder(r io.Reader, w io.Writer) (err os.Error) {

	// set fields of z
	// start decoding

	return nil
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
