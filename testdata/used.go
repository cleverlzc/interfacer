package foo

import (
	"io"
	"os"
)

func FooCloser(c io.Closer) {
	c.Close()
}

func FooFile(f *os.File) {
	f.Stat()
}

func Bar(f *os.File) {
	f.Close()
	FooFile(f)
}

func BarWrong(f *os.File) {
	f.Close()
	FooCloser(f)
}

func Extra(n int, cs ...io.Closer) {}

func ArgExtraWrong(f1 *os.File) {
	var f2 *os.File
	f1.Close()
	f2.Close()
	Extra(3, f1, f2)
}

func Assigned(f *os.File) {
	f.Close()
	var f2 *os.File
	f2 = f
	println(f2)
}

func AssignedWrong(f *os.File) {
	f.Close()
	var c io.Closer
	c = f
	println(c)
}
