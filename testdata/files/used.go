package foo

type Closer interface {
	Close() error
}

type Reader interface {
	Read(p []byte) (n int, err error)
}

type ReadCloser interface {
	Reader
	Closer
}

type st struct{}

func (s st) Bang() {}
func (s st) Close() error {
	return nil
}
func (s st) Other() {}

func FooCloser(c Closer) {
	c.Close()
}

func FooSt(s st) {
	s.Other()
}

func Bar(s st) {
	s.Close()
	FooSt(s)
}

func BarWrong(s st) {
	s.Close()
	FooCloser(s)
}

func Extra(n int, cs ...Closer) {}

func ArgExtraWrong(s1 st) {
	var s2 st
	s1.Close()
	s2.Close()
	Extra(3, s1, s2)
}

func Assigned(s st) {
	s.Close()
	var s2 st
	s2 = s
	_ = s2
}

func AssignedWrong(s st) {
	s.Close()
	var c Closer
	c = s
	_ = c
}

type BangCloser interface {
	Closer
	Bang()
}

func Bang(bc BangCloser) {
	var bc2 BangCloser
	bc.Close()
	bc2 = bc
	bc2.Bang()
}

func BangWrong(bc BangCloser) {
	bc.Close()
}

type Banger interface {
	Bang()
}

func BangLighter(s st) {
	s.Close()
	var b Banger
	b = s
	b.Bang()
}

func BangLighterWrong(s st) {
	s.Bang()
	s.Close()
	var c Closer
	c = s
	c.Close()
}

func CompareNilWrong(rc ReadCloser) {
	if rc != nil {
		rc.Close()
	}
}
