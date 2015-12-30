package pkg

type Closer interface {
	Close()
}

type ReadCloser interface {
	Closer
	Read()
}

func BasicWrong(rc ReadCloser) {
	rc.Close()
}
