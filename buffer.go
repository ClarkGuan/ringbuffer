package bring

import (
	"bytes"
	"container/ring"
	"errors"
	"io"
	"net"
	"reflect"
	"unsafe"

	assert "github.com/ClarkGuan/assertgo"
)

// ring buffer
type Buffer struct {
	rg     *ring.Ring
	pr, pw struct {
		r *ring.Ring
		i int
	}
	bufSize int
	left    int
	cap     int
}

func New(size, n int) *Buffer {
	if size < 512 {
		size = 512
	}

	if n < 2 {
		n = 2
	}

	rb := Buffer{}
	rb.bufSize = size

	newR := ring.New(n)
	newR.Value = make([]byte, size)
	for p := newR.Next(); p != newR; p = p.Next() {
		p.Value = make([]byte, size)
	}
	rb.rg = newR

	rb.Reset()
	return &rb
}

func (rb *Buffer) Reset() {
	rb.pw.r = rb.rg
	rb.pw.i = 0
	rb.pr.r = rb.rg
	rb.pr.i = 0
	rb.left = 0
	rb.cap = rb.rg.Len() * rb.bufSize
}

func (rb *Buffer) Truncate(n int) {
	if n <= 0 {
		rb.Reset()
		return
	}

	if n >= rb.left {
		return
	}

	rb.cap += rb.left - n
	rb.left = n

	rb.pw.i = rb.pr.i
	rb.pw.r = rb.pr.r
	m := n

	for m > 0 {
		if rb.bufSize-rb.pw.i < m {
			rb.pw.i = 0
			rb.pw.r = rb.pw.r.Next()
			m -= rb.bufSize - rb.pw.i
		} else {
			rb.pw.i += m
			break
		}
	}
}

func (rb *Buffer) Close() error {
	rb.Reset()
	return nil
}

// how many bytes can write to without grow
func (rb *Buffer) Cap() int {
	return rb.cap
}

// how many bytes can read from
func (rb *Buffer) Len() int {
	return rb.left
}

func (rb *Buffer) Read(p []byte) (n int, err error) {
	if rb.left == 0 {
		return 0, io.EOF
	}

	if rb.left > len(p) {
		n = len(p)
	} else {
		n = rb.left
	}

	var buf []byte
	var offset, count, end int
	for {
		if rb.pr.r == rb.pw.r {
			end = rb.pw.i
		} else {
			end = rb.bufSize
		}
		buf = ringBytes(rb.pr.r)
		count = copy(p[offset:], buf[rb.pr.i:end])
		offset += count
		rb.pr.i += count
		rb.left -= count

		if rb.left == 0 || offset == len(p) {
			rb.stepNext(false)
			break
		}

		rb.stepNext(true)
	}

	return
}

func (rb *Buffer) Write(p []byte) (n int, err error) {
	if rb.cap < len(p) {
		rb.grow(len(p) - rb.cap)
		assert.True(rb.cap >= len(p))
	}

	total := len(p)
	var buf []byte
	var cnt, offset int

	for {
		buf = ringBytes(rb.pw.r)
		cnt = copy(buf[rb.pw.i:], p[offset:])
		rb.pw.i += cnt
		offset += cnt
		rb.cap -= cnt
		rb.left += cnt

		if total == offset {
			break
		}
		assert.True(rb.pw.i == rb.bufSize)

		rb.pw.i = 0
		rb.pw.r = rb.pw.r.Next()
	}

	return total, nil
}

func (rb *Buffer) WriteString(s string) (n int, err error) {
	var buf []byte
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&buf))
	bh.Data = sh.Data
	bh.Cap = sh.Len
	bh.Len = sh.Len
	return rb.Write(buf)
}

func (rb *Buffer) writeTo(w io.Writer) (n int64, err error) {
	pr, pw, c, l := rb.pr, rb.pw, rb.cap, rb.left
	n, err = rb.WriteTo(w)
	rb.pr, rb.pw, rb.cap, rb.left = pr, pw, c, l
	return
}

func (rb *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	if rb.left == 0 {
		return
	}

	// 如果缓冲区大小不超过一个 ring node，则走优化路径
	if rb.pr.i+rb.left <= rb.bufSize {
		var cnt int
		cnt, err = w.Write(ringBytes(rb.pr.r)[rb.pr.i : rb.pr.i+rb.left])
		if cnt < 0 {
			cnt = 0
		}
		rb.pr.i += cnt
		rb.left -= cnt
		return int64(cnt), err
	}

	var bufList [][]byte
	var buf []byte
	var start, end, total int

	prg := rb.pr.r
	total = rb.left

	for total > 0 {
		if prg == rb.pw.r {
			end = rb.pw.i
		} else {
			end = rb.bufSize
		}

		if prg == rb.pr.r {
			start = rb.pr.i
		} else {
			start = 0
		}

		buf = ringBytes(prg)[start:end]
		bufList = append(bufList, buf)

		total -= len(buf)
		prg = prg.Next()
	}

	// 对网络套接字有优化
	buffers := net.Buffers(bufList)
	n, err = buffers.WriteTo(w)
	if n <= 0 {
		return
	}

	nn := int(n)
	rb.left -= nn
	// 重新计算 rb.pr
	if rb.pr.i+nn <= rb.bufSize {
		rb.pr.i += nn
		rb.stepNext(false)
	} else {
		nn -= rb.bufSize - rb.pr.i
		tail := nn % rb.bufSize
		cnt := nn / rb.bufSize
		if tail > 0 {
			cnt++
			for cnt > 0 {
				rb.pr.r = rb.pr.r.Next()
				rb.cap += rb.bufSize
				cnt--
			}
			rb.pr.i = tail
		} else {
			for cnt > 0 {
				rb.pr.r = rb.pr.r.Next()
				rb.cap += rb.bufSize
				cnt--
			}
			rb.pr.i = rb.bufSize
			rb.stepNext(false)
		}
	}

	return
}

func (rb *Buffer) ReadFrom(r io.Reader) (total int64, err error) {
	// 不确定可以从 r 中读到多少数据，这里只能一点一点增加容量
	var n int
	for {
		buf := ringBytes(rb.pw.r)[rb.pw.i:]
		n, err = r.Read(buf)
		if n < 0 {
			panic(errors.New("bring.Buffer: reader returned negative count from Read"))
		}
		total += int64(n)
		rb.cap -= n
		rb.pw.i += n
		rb.left += n

		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return
		}

		if rb.cap == 0 {
			rb.grow(1)
		}

		if rb.pw.i == rb.bufSize {
			rb.pw.i = 0
			rb.pw.r = rb.pw.r.Next()
		}
	}
}

func (rb *Buffer) ReadByte() (byte, error) {
	if rb.left == 0 {
		return 0, io.EOF
	}

	buf := ringBytes(rb.pr.r)
	i := rb.pr.i
	rb.pr.i++
	rb.left--

	if rb.left != 0 {
		if rb.pr.i == rb.bufSize {
			rb.stepNext(true)
		}
	}

	return buf[i], nil
}

func (rb *Buffer) WriteByte(c byte) error {
	if rb.cap == 0 {
		rb.grow(1)
	}
	if rb.pw.i == rb.bufSize {
		rb.pw.i = 0
		rb.pw.r = rb.pw.r.Next()
	}
	ringBytes(rb.pw.r)[rb.pw.i] = c
	rb.pw.i++
	rb.left++
	rb.cap--
	return nil
}

func (rb *Buffer) GoString() string {
	return rb.String()
}

func (rb *Buffer) String() string {
	return string(rb.Bytes())
}

func (rb *Buffer) Bytes() []byte {
	if rb.left == 0 {
		return nil
	}
	buf := bytes.NewBuffer(make([]byte, 0, rb.left))
	rb.writeTo(buf)
	return buf.Bytes()
}

// read pointer step into next ring node
func (rb *Buffer) stepNext(check bool) {
	if rb.pr.i != rb.bufSize || rb.pr.r == rb.pw.r {
		if check {
			assert.True(false)
		} else {
			return
		}
	}

	rb.pr.i = 0
	rb.pr.r = rb.pr.r.Next()
	rb.cap += rb.bufSize
}

// the number of ring elements grow to satisfy the need of extra size n (bytes)
func (rb *Buffer) grow(n int) {
	m := n/rb.bufSize + 1
	newR := ring.New(m)
	newR.Value = make([]byte, rb.bufSize)
	for p := newR.Next(); p != newR; p = p.Next() {
		p.Value = make([]byte, rb.bufSize)
	}
	rb.cap += m * rb.bufSize
	rb.pw.r.Link(newR) // insert new ring elements
}

func ringBytes(r *ring.Ring) []byte {
	return r.Value.([]byte)
}
