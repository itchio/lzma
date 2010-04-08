/*
The zlib package implements reading and writing of zlib
format compressed data, as specified in RFC 1950.

The implementation provides filters that uncompress during reading
and compress during writing.  For example, to write compressed data
to a buffer:

        var b bytes.Buffer
        w, err := zlib.NewDeflater(&b)
        w.Write([]byte("hello, world\n"))
        w.Close()

and to read that data back:

        r, err := zlib.NewInflater(&b)
        io.Copy(os.Stdout, r)
        r.Close()
*/
package lzma

import (
	"io"
	"os"
)

type decoder struct {	// flate.inflater, zlib.reader, gzip.inflater
	compressionLevel
	r	io.Reader
	w	io.Writer
	size	uint64
	eos	bool
	err     os.Error
}

func NewDecoder(w io.Reader) (io.ReadCloser, os.Error) {
	return nil, nil
}
