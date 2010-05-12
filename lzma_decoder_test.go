// Copyright (c) 2010, Andrei Vieru. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzma

import (
	"bytes"
	"io"
	"log"
	"reflect"
	"testing"
)

func TestDecoder(t *testing.T) {
	b := new(bytes.Buffer)
	for _, tt := range lzmaTests {
		in := bytes.NewBuffer(tt.lzma)
		r := NewDecoder(in)
		defer r.Close()
		b.Reset()
		n, err := io.Copy(b, r)
		if err != tt.err {
			t.Errorf("%s: io.Copy: %v, want %v", tt.descr, err, tt.err)
		}
		if err == nil { // if err != nil, there is little chance that data is decoded correctly, if at all
			s := b.String()
			if s != tt.raw {
				t.Errorf("%s: got %d-byte %q, want %d-byte %q", tt.descr, n, s, len(tt.raw), tt.raw)
			}
		}
	}
}

func BenchmarkDecoder(b *testing.B) {
	b.StopTimer()
	buf := new(bytes.Buffer)
	for i := 0; i < b.N; i++ {
		buf.Reset()
		in := bytes.NewBuffer(bk.lzma)
		b.StartTimer()
		r := NewDecoder(in)
		n, err := io.Copy(buf, r)
		b.StopTimer()
		if err != nil {
			log.Exitf("%v", err)
		}
		b.SetBytes(n)
		r.Close()
	}
	if reflect.DeepEqual(buf.Bytes(), bk.raw) == false { // check only after last iteration
		log.Exitf("%s: got %d-byte %q, want %d-byte %q", bk.descr, len(buf.Bytes()), buf.String(), len(bk.raw), bk.raw)
	}
}
