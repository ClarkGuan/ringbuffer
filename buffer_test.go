package bring_test

import (
	"io"
	"math/rand"
	"testing"

	bring "github.com/ClarkGuan/ringbuffer"
)

const N = 10000       // make this bigger for a larger (and slower) test
var testString string // test data for write tests
var testBytes []byte  // test data; same as testString but as a slice.

type negativeReader struct{}

func (r *negativeReader) Read([]byte) (int, error) { return -1, nil }

func init() {
	testBytes = make([]byte, N)
	for i := 0; i < N; i++ {
		testBytes[i] = 'a' + byte(i%26)
	}
	testString = string(testBytes)
}

// Verify that contents of buf match the string s.
func check(t *testing.T, testname string, buf *bring.Buffer, s string) {
	bytes := buf.Bytes()
	str := buf.String()
	if buf.Len() != len(bytes) {
		t.Errorf("%s: buf.Len() == %d, len(buf.Bytes()) == %d", testname, buf.Len(), len(bytes))
	}

	if buf.Len() != len(str) {
		t.Errorf("%s: buf.Len() == %d, len(buf.String()) == %d", testname, buf.Len(), len(str))
	}

	if buf.Len() != len(s) {
		t.Errorf("%s: buf.Len() == %d, len(s) == %d", testname, buf.Len(), len(s))
	}

	if string(bytes) != s {
		t.Errorf("%s: string(buf.Bytes()) == %q, s == %q", testname, string(bytes), s)
	}
}

// Fill buf through n writes of string fus.
// The initial contents of buf corresponds to the string s;
// the result is the final contents of buf returned as a string.
func fillString(t *testing.T, testname string, buf *bring.Buffer, s string, n int, fus string) string {
	check(t, testname+" (fill 1)", buf, s)
	for ; n > 0; n-- {
		m, err := buf.WriteString(fus)
		if m != len(fus) {
			t.Errorf(testname+" (fill 2): m == %d, expected %d", m, len(fus))
		}
		if err != nil {
			t.Errorf(testname+" (fill 3): err should always be nil, found err == %s", err)
		}
		s += fus
		check(t, testname+" (fill 4)", buf, s)
	}
	return s
}

// Fill buf through n writes of byte slice fub.
// The initial contents of buf corresponds to the string s;
// the result is the final contents of buf returned as a string.
func fillBytes(t *testing.T, testname string, buf *bring.Buffer, s string, n int, fub []byte) string {
	check(t, testname+" (fill 1)", buf, s)
	for ; n > 0; n-- {
		m, err := buf.Write(fub)
		if m != len(fub) {
			t.Errorf(testname+" (fill 2): m == %d, expected %d", m, len(fub))
		}
		if err != nil {
			t.Errorf(testname+" (fill 3): err should always be nil, found err == %s", err)
		}
		s += string(fub)
		check(t, testname+" (fill 4)", buf, s)
	}
	return s
}

func TestNewBuffer(t *testing.T) {
	buf := bring.New()
	if buf.Len() != 0 {
		t.Fail()
	}

	if buf.Cap() != 512 {
		t.Fail()
	}
}

// Empty buf through repeated reads into fub.
// The initial contents of buf corresponds to the string s.
func empty(t *testing.T, testname string, buf *bring.Buffer, s string, fub []byte) {
	check(t, testname+" (empty 1)", buf, s)

	for {
		n, err := buf.Read(fub)
		if n == 0 {
			break
		}
		if err != nil {
			t.Errorf(testname+" (empty 2): err should always be nil, found err == %s", err)
		}
		s = s[n:]
		check(t, testname+" (empty 3)", buf, s)
	}

	check(t, testname+" (empty 4)", buf, "")
}

func TestBasicOperations(t *testing.T) {
	buf := bring.New()

	for i := 0; i < 5; i++ {
		check(t, "TestBasicOperations (1)", buf, "")

		buf.Reset()
		check(t, "TestBasicOperations (2)", buf, "")

		buf.Truncate(0)
		check(t, "TestBasicOperations (3)", buf, "")

		n, err := buf.Write(testBytes[0:1])
		if want := 1; err != nil || n != want {
			t.Errorf("Write: got (%d, %v), want (%d, %v)", n, err, want, nil)
		}
		check(t, "TestBasicOperations (4)", buf, "a")

		buf.WriteByte(testString[1])
		check(t, "TestBasicOperations (5)", buf, "ab")

		n, err = buf.Write(testBytes[2:26])
		if want := 24; err != nil || n != want {
			t.Errorf("Write: got (%d, %v), want (%d, %v)", n, err, want, nil)
		}
		check(t, "TestBasicOperations (6)", buf, testString[0:26])

		buf.Truncate(26)
		check(t, "TestBasicOperations (7)", buf, testString[0:26])

		buf.Truncate(20)
		check(t, "TestBasicOperations (8)", buf, testString[0:20])

		empty(t, "TestBasicOperations (9)", buf, testString[0:20], make([]byte, 5))
		empty(t, "TestBasicOperations (10)", buf, "", make([]byte, 100))

		buf.WriteByte(testString[1])
		c, err := buf.ReadByte()
		if want := testString[1]; err != nil || c != want {
			t.Errorf("ReadByte: got (%q, %v), want (%q, %v)", c, err, want, nil)
		}
		c, err = buf.ReadByte()
		if err != io.EOF {
			t.Errorf("ReadByte: got (%q, %v), want (%q, %v)", c, err, byte(0), io.EOF)
		}
	}
}

func TestLargeStringWrites(t *testing.T) {
	buf := bring.New()
	limit := 30
	if testing.Short() {
		limit = 9
	}
	for i := 3; i < limit; i += 3 {
		s := fillString(t, "TestLargeWrites (1)", buf, "", 5, testString)
		empty(t, "TestLargeStringWrites (2)", buf, s, make([]byte, len(testString)/i))
	}
	check(t, "TestLargeStringWrites (3)", buf, "")
}

func TestLargeByteWrites(t *testing.T) {
	buf := bring.New()
	limit := 30
	if testing.Short() {
		limit = 9
	}
	for i := 3; i < limit; i += 3 {
		s := fillBytes(t, "TestLargeWrites (1)", buf, "", 5, testBytes)
		empty(t, "TestLargeByteWrites (2)", buf, s, make([]byte, len(testString)/i))
	}
	check(t, "TestLargeByteWrites (3)", buf, "")
}

func TestLargeStringReads(t *testing.T) {
	buf := bring.New()
	for i := 3; i < 30; i += 3 {
		s := fillString(t, "TestLargeReads (1)", buf, "", 5, testString[0:len(testString)/i])
		empty(t, "TestLargeReads (2)", buf, s, make([]byte, len(testString)))
	}
	check(t, "TestLargeStringReads (3)", buf, "")
}

func TestLargeByteReads(t *testing.T) {
	buf := bring.New()
	for i := 3; i < 30; i += 3 {
		s := fillBytes(t, "TestLargeReads (1)", buf, "", 5, testBytes[0:len(testBytes)/i])
		empty(t, "TestLargeReads (2)", buf, s, make([]byte, len(testString)))
	}
	check(t, "TestLargeByteReads (3)", buf, "")
}

func TestMixedReadsAndWrites(t *testing.T) {
	buf := bring.New()
	s := ""
	for i := 0; i < 50; i++ {
		wlen := rand.Intn(len(testString))
		if i%2 == 0 {
			s = fillString(t, "TestMixedReadsAndWrites (1)", buf, s, 1, testString[0:wlen])
		} else {
			s = fillBytes(t, "TestMixedReadsAndWrites (1)", buf, s, 1, testBytes[0:wlen])
		}

		rlen := rand.Intn(len(testString))
		fub := make([]byte, rlen)
		n, _ := buf.Read(fub)
		s = s[n:]
	}
	empty(t, "TestMixedReadsAndWrites (2)", buf, s, make([]byte, buf.Len()))
}

func TestCapWithPreallocatedSlice(t *testing.T) {
	buf := bring.New()
	n := buf.Cap()
	if n != 512 {
		t.Errorf("expected 512, got %d", n)
	}
}

func TestCapWithSliceAndWrittenData(t *testing.T) {
	buf := bring.New()
	buf.Write([]byte("test"))
	n := buf.Cap()
	if n != 508 {
		t.Errorf("expected 508, got %d", n)
	}
}

func TestNil(t *testing.T) {
	b := bring.New()
	if b.String() != "" {
		t.Errorf("expected \"\"; got %q", b.String())
	}
}

func TestReadFrom(t *testing.T) {
	buf := bring.New()
	for i := 3; i < 30; i += 3 {
		s := fillBytes(t, "TestReadFrom (1)", buf, "", 5, testBytes[0:len(testBytes)/i])
		b := bring.New()
		b.ReadFrom(buf)
		empty(t, "TestReadFrom (2)", b, s, make([]byte, len(testString)))
	}
}

type panicReader struct{ panic bool }

func (r panicReader) Read(p []byte) (int, error) {
	if r.panic {
		panic(nil)
	}
	return 0, io.EOF
}

// Make sure that an empty Buffer remains empty when
// it is "grown" before a Read that panics
func TestReadFromPanicReader(t *testing.T) {
	// First verify non-panic behaviour
	buf := bring.New()
	i, err := buf.ReadFrom(panicReader{})
	if err != nil {
		t.Fatal(err)
	}
	if i != 0 {
		t.Fatalf("unexpected return from bytes.ReadFrom (1): got: %d, want %d", i, 0)
	}
	check(t, "TestReadFromPanicReader (1)", buf, "")

	// Confirm that when Reader panics, the empty buffer remains empty
	buf2 := bring.New()
	defer func() {
		recover()
		check(t, "TestReadFromPanicReader (2)", buf2, "")
	}()
	buf2.ReadFrom(panicReader{panic: true})
}

func TestReadFromNegativeReader(t *testing.T) {
	b := bring.New()
	defer func() {
		switch err := recover().(type) {
		case nil:
			t.Fatal("bytes.Buffer.ReadFrom didn't panic")
		case error:
			// this is the error string of errNegativeRead
			wantError := "bring.Buffer: reader returned negative count from Read"
			if err.Error() != wantError {
				t.Fatalf("recovered panic: got %v, want %v", err.Error(), wantError)
			}
		default:
			t.Fatalf("unexpected panic value: %#v", err)
		}
	}()

	b.ReadFrom(new(negativeReader))
}

func TestWriteTo(t *testing.T) {
	buf := bring.New()
	for i := 3; i < 30; i += 3 {
		s := fillBytes(t, "TestWriteTo (1)", buf, "", 5, testBytes[0:len(testBytes)/i])
		b := bring.New()
		buf.WriteTo(b)
		empty(t, "TestWriteTo (2)", b, s, make([]byte, len(testString)))
	}
}

//func TestGrow(t *testing.T) {
//	x := []byte{'x'}
//	y := []byte{'y'}
//	tmp := make([]byte, 72)
//	for _, startLen := range []int{0, 100, 1000, 10000, 100000} {
//		xBytes := bytes.Repeat(x, startLen)
//		for _, growLen := range []int{0, 100, 1000, 10000, 100000} {
//			buf := bytes.NewBuffer(xBytes)
//			// If we read, this affects buf.off, which is good to test.
//			readBytes, _ := buf.Read(tmp)
//			buf.Grow(growLen)
//			yBytes := bytes.Repeat(y, growLen)
//			// Check no allocation occurs in write, as long as we're single-threaded.
//			var m1, m2 runtime.MemStats
//			runtime.ReadMemStats(&m1)
//			buf.Write(yBytes)
//			runtime.ReadMemStats(&m2)
//			if runtime.GOMAXPROCS(-1) == 1 && m1.Mallocs != m2.Mallocs {
//				t.Errorf("allocation occurred during write")
//			}
//			// Check that buffer has correct data.
//			if !bytes.Equal(buf.Bytes()[0:startLen-readBytes], xBytes[readBytes:]) {
//				t.Errorf("bad initial data at %d %d", startLen, growLen)
//			}
//			if !bytes.Equal(buf.Bytes()[startLen-readBytes:startLen-readBytes+growLen], yBytes) {
//				t.Errorf("bad written data at %d %d", startLen, growLen)
//			}
//		}
//	}
//}

//func TestGrowOverflow(t *testing.T) {
//	defer func() {
//		if err := recover(); err != bytes.ErrTooLarge {
//			t.Errorf("after too-large Grow, recover() = %v; want %v", err, bytes.ErrTooLarge)
//		}
//	}()
//
//	buf := bytes.NewBuffer(make([]byte, 1))
//	const maxInt = int(^uint(0) >> 1)
//	buf.Grow(maxInt)
//}
//
//// Was a bug: used to give EOF reading empty slice at EOF.
//func TestReadEmptyAtEOF(t *testing.T) {
//	b := new(bytes.Buffer)
//	slice := make([]byte, 0)
//	n, err := b.Read(slice)
//	if err != nil {
//		t.Errorf("read error: %v", err)
//	}
//	if n != 0 {
//		t.Errorf("wrong count; got %d want 0", n)
//	}
//}
//
//func TestUnreadByte(t *testing.T) {
//	b := new(bytes.Buffer)
//
//	// check at EOF
//	if err := b.UnreadByte(); err == nil {
//		t.Fatal("UnreadByte at EOF: got no error")
//	}
//	if _, err := b.ReadByte(); err == nil {
//		t.Fatal("ReadByte at EOF: got no error")
//	}
//	if err := b.UnreadByte(); err == nil {
//		t.Fatal("UnreadByte after ReadByte at EOF: got no error")
//	}
//
//	// check not at EOF
//	b.WriteString("abcdefghijklmnopqrstuvwxyz")
//
//	// after unsuccessful read
//	if n, err := b.Read(nil); n != 0 || err != nil {
//		t.Fatalf("Read(nil) = %d,%v; want 0,nil", n, err)
//	}
//	if err := b.UnreadByte(); err == nil {
//		t.Fatal("UnreadByte after Read(nil): got no error")
//	}
//
//	// after successful read
//	if _, err := b.ReadBytes('m'); err != nil {
//		t.Fatalf("ReadBytes: %v", err)
//	}
//	if err := b.UnreadByte(); err != nil {
//		t.Fatalf("UnreadByte: %v", err)
//	}
//	c, err := b.ReadByte()
//	if err != nil {
//		t.Fatalf("ReadByte: %v", err)
//	}
//	if c != 'm' {
//		t.Errorf("ReadByte = %q; want %q", c, 'm')
//	}
//}
//
//// Tests that we occasionally compact. Issue 5154.
//func TestBufferGrowth(t *testing.T) {
//	var b bytes.Buffer
//	buf := make([]byte, 1024)
//	b.Write(buf[0:1])
//	var cap0 int
//	for i := 0; i < 5<<10; i++ {
//		b.Write(buf)
//		b.Read(buf)
//		if i == 0 {
//			cap0 = b.Cap()
//		}
//	}
//	cap1 := b.Cap()
//	// (*Buffer).grow allows for 2x capacity slop before sliding,
//	// so set our error threshold at 3x.
//	if cap1 > cap0*3 {
//		t.Errorf("buffer cap = %d; too big (grew from %d)", cap1, cap0)
//	}
//}
//
//func BenchmarkWriteByte(b *testing.B) {
//	const n = 4 << 10
//	b.SetBytes(n)
//	buf := bytes.NewBuffer(make([]byte, n))
//	for i := 0; i < b.N; i++ {
//		buf.Reset()
//		for i := 0; i < n; i++ {
//			buf.WriteByte('x')
//		}
//	}
//}
//
//func BenchmarkWriteRune(b *testing.B) {
//	const n = 4 << 10
//	const r = '☺'
//	b.SetBytes(int64(n * utf8.RuneLen(r)))
//	buf := bytes.NewBuffer(make([]byte, n*utf8.UTFMax))
//	for i := 0; i < b.N; i++ {
//		buf.Reset()
//		for i := 0; i < n; i++ {
//			buf.WriteRune(r)
//		}
//	}
//}
//
//// From Issue 5154.
//func BenchmarkBufferNotEmptyWriteRead(b *testing.B) {
//	buf := make([]byte, 1024)
//	for i := 0; i < b.N; i++ {
//		var b bytes.Buffer
//		b.Write(buf[0:1])
//		for i := 0; i < 5<<10; i++ {
//			b.Write(buf)
//			b.Read(buf)
//		}
//	}
//}
//
//// Check that we don't compact too often. From Issue 5154.
//func BenchmarkBufferFullSmallReads(b *testing.B) {
//	buf := make([]byte, 1024)
//	for i := 0; i < b.N; i++ {
//		var b bytes.Buffer
//		b.Write(buf)
//		for b.Len()+20 < b.Cap() {
//			b.Write(buf[:10])
//		}
//		for i := 0; i < 5<<10; i++ {
//			b.Read(buf[:1])
//			b.Write(buf[:1])
//		}
//	}
//}
