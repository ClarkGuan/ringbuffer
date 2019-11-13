package bring

import (
	"bytes"
	"container/ring"
	"errors"
	"io"
	"net"

	assert "github.com/ClarkGuan/assertgo"
)

const (
	MinBlockSize  = 512
	MinBlockCount = 1
)

// ring buffer
type Buffer struct {
	pr, pw struct {
		r *ring.Ring
		i int
	}
	blockSize int
	left      int
	cap       int
	maxCap    int
}

// ns[0] 表示每个缓冲区的最大大小；ns[1] 表示缓冲区的个数
func New(ns ...int) *Buffer {
	var size, n int

	if len(ns) == 1 {
		size = ns[0]
	} else if len(ns) > 1 {
		size = ns[0]
		n = ns[1]
	}

	if size < MinBlockSize {
		size = MinBlockSize
	}

	if n < MinBlockCount {
		n = MinBlockCount
	}

	rb := Buffer{}
	rb.blockSize = size

	newR := ring.New(n)
	newR.Value = make([]byte, size)
	for p := newR.Next(); p != newR; p = p.Next() {
		p.Value = make([]byte, size)
	}
	rb.maxCap = size * n
	rb.pw.r = newR

	rb.Reset()
	return &rb
}

func (rb *Buffer) Reset() {
	rb.pw.i = 0
	rb.pr.r = rb.pw.r
	rb.pr.i = 0
	rb.left = 0
	rb.cap = rb.maxCap
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
		if rb.blockSize-rb.pw.i < m {
			rb.pw.i = 0
			rb.pw.r = rb.pw.r.Next()
			m -= rb.blockSize - rb.pw.i
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

func (rb *Buffer) MaxCap() int {
	return rb.maxCap
}

// how many bytes can read from
func (rb *Buffer) Len() int {
	return rb.left
}

func (rb *Buffer) Skip(n int) {
	if n > rb.left {
		n = rb.left
	}

	if n <= 0 {
		return
	}

	rb.left -= n
	if rb.left == 0 {
		rb.Reset()
		return
	}

	// 重新计算 rb.pr
	if rb.pr.i+n <= rb.blockSize {
		rb.pr.i += n
		rb.stepNext(false)
	} else {
		n -= rb.blockSize - rb.pr.i
		tail := n % rb.blockSize
		cnt := n / rb.blockSize
		if tail > 0 {
			cnt++
			for cnt > 0 {
				rb.pr.r = rb.pr.r.Next()
				rb.cap += rb.blockSize
				cnt--
			}
			rb.pr.i = tail
		} else {
			for cnt > 0 {
				rb.pr.r = rb.pr.r.Next()
				rb.cap += rb.blockSize
				cnt--
			}
			rb.pr.i = rb.blockSize
			rb.stepNext(false)
		}
	}
}

func (rb *Buffer) Peek() (list [][]byte) {
	if rb.left == 0 {
		return
	}

	var buf []byte
	var start, end, total int

	prg := rb.pr.r
	total = rb.left

	for total > 0 {
		if prg == rb.pw.r {
			end = rb.pw.i
		} else {
			end = rb.blockSize
		}

		if prg == rb.pr.r {
			start = rb.pr.i
		} else {
			start = 0
		}

		buf = ringBytes(prg)[start:end]
		list = append(list, buf)

		total -= len(buf)
		prg = prg.Next()
	}

	return
}

func (rb *Buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

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
			end = rb.blockSize
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

	if rb.left == 0 {
		rb.Reset()
		return
	}

	return
}

func (rb *Buffer) Write(p []byte) (n int, err error) {
	lenp := len(p)
	if rb.cap < lenp {
		rb.grow(lenp - rb.cap)
		assert.True(rb.cap >= lenp)
	}

	total := lenp
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
		assert.True(rb.pw.i == rb.blockSize)

		rb.pw.i = 0
		rb.pw.r = rb.pw.r.Next()
	}

	return total, nil
}

func (rb *Buffer) WriteString(s string) (n int, err error) {
	lens := len(s)
	if rb.cap < lens {
		rb.grow(lens - rb.cap)
		assert.True(rb.cap >= lens)
	}

	total := lens
	var buf []byte
	var cnt, offset int

	for {
		buf = ringBytes(rb.pw.r)
		cnt = copy(buf[rb.pw.i:], s[offset:])
		rb.pw.i += cnt
		offset += cnt
		rb.cap -= cnt
		rb.left += cnt

		if total == offset {
			break
		}
		assert.True(rb.pw.i == rb.blockSize)

		rb.pw.i = 0
		rb.pw.r = rb.pw.r.Next()
	}

	return total, nil
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
	if rb.pr.i+rb.left <= rb.blockSize {
		var cnt int
		cnt, err = w.Write(ringBytes(rb.pr.r)[rb.pr.i : rb.pr.i+rb.left])
		if cnt < 0 {
			cnt = 0
		}
		rb.pr.i += cnt
		rb.left -= cnt
		if rb.left == 0 {
			rb.Reset()
		}
		return int64(cnt), err
	}

	// 对网络套接字有优化
	buffers := net.Buffers(rb.Peek())
	n, err = buffers.WriteTo(w)
	rb.Skip(int(n))

	return
}

func (rb *Buffer) readFrom(r io.Reader, pipe bool) (total int64, err error) {
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

		if err != nil || (pipe && n < len(buf)) { // 如果 pipe 类 fd 读取完成
			if !pipe && err == io.EOF {
				err = nil
			}
			return
		}

		if rb.cap == 0 {
			rb.grow(1)
		}

		if rb.pw.i == rb.blockSize {
			rb.pw.i = 0
			rb.pw.r = rb.pw.r.Next()
		}
	}
}

func (rb *Buffer) ReadFrom(r io.Reader) (total int64, err error) {
	return rb.readFrom(r, false)
}

func (rb *Buffer) ReadFromPipe(r io.Reader) (int64, error) {
	return rb.readFrom(r, true)
}

func (rb *Buffer) ReadByte() (byte, error) {
	if rb.left == 0 {
		return 0, io.EOF
	}

	c := ringBytes(rb.pr.r)[rb.pr.i]
	rb.pr.i++
	rb.left--

	if rb.left == 0 {
		rb.Reset()
	} else {
		if rb.pr.i == rb.blockSize {
			rb.stepNext(true)
		}
	}

	return c, nil
}

func (rb *Buffer) WriteByte(c byte) error {
	if rb.cap == 0 {
		rb.grow(1)
	}
	if rb.pw.i == rb.blockSize {
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
	if rb.pr.i != rb.blockSize || rb.pr.r == rb.pw.r {
		if check {
			assert.True(false)
		} else {
			return
		}
	}

	rb.pr.i = 0
	rb.pr.r = rb.pr.r.Next()
	rb.cap += rb.blockSize
}

// the number of ring elements grow to satisfy the need of extra size n (bytes)
func (rb *Buffer) grow(n int) {
	m := n/rb.blockSize + 1
	newR := ring.New(m)
	newR.Value = make([]byte, rb.blockSize)
	for p := newR.Next(); p != newR; p = p.Next() {
		p.Value = make([]byte, rb.blockSize)
	}
	delta := m * rb.blockSize
	rb.cap += delta
	rb.maxCap += delta
	rb.pw.r.Link(newR) // insert new ring elements
}

func ringBytes(r *ring.Ring) []byte {
	return r.Value.([]byte)
}
