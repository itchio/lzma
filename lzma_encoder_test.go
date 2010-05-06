// Copyright (c) 2010, Andrei Vieru. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzma

import (
	"io"
	"io/ioutil"
	"testing"
)

func pipe(t *testing.T, efunc func(io.WriteCloser), dfunc func(io.ReadCloser), size int64) {
	level := 4
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		ze := NewEncoderSizeLevel(pw, size, level)
		defer ze.Close()
		efunc(ze)
	}()
	defer pr.Close()
	zd := NewDecoder(pr)
	defer zd.Close()
	dfunc(zd)
}

func testEmpty(t *testing.T, sizeIsKnown bool) {
	size := int64(-1)
	if sizeIsKnown == true {
		size = 0
	}
	pipe(t,
		func(w io.WriteCloser) {},
		func(r io.ReadCloser) {
			b, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if len(b) != 0 {
				t.Fatalf("did not read an empty slice")
			}
		},
		size)
}

func TestEmpty1(t *testing.T) {
	testEmpty(t, true)
}

func TestEmpty2(t *testing.T) {
	testEmpty(t, false)
}

func testBoth(t *testing.T, sizeIsKnown bool) {
	size := int64(-1)
	payload := []byte("lzmalzmalzma")
	if sizeIsKnown == true {
		size = int64(len(payload))
	}
	pipe(t,
		func(w io.WriteCloser) {
			n, err := w.Write(payload)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if n != len(payload) {
				t.Fatalf("wrote %d bytes, want %d bytes", n, len(payload))
			}
		},
		func(r io.ReadCloser) {
			b, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if string(b) != string(payload) {
				t.Fatalf("payload is %s, want %s", string(b), string(payload))
			}
		},
		size)
}

func TestBoth1(t *testing.T) {
	testBoth(t, true)
}

func TestBoth2(t *testing.T) {
	testBoth(t, false)
}
