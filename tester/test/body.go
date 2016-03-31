package test

import (
	"bytes"
	"io"

	"github.com/seehuhn/mt19937"
)

// ResponseBodySpec describes a (synthetic) server response.
type ResponseBodySpec struct {
	// Seed is used to initialise the random number generator for
	// generating the response.  Small positive integers should be to
	// select which response to generate.
	Seed int64

	// Length specifies the required response body size in bytes.  The
	// length does not include the response header.
	Length int64
}

func (task *ResponseBodySpec) Write(w io.Writer) (int64, error) {
	rng := mt19937.New()
	rng.Seed(task.Seed)
	return io.CopyN(w, rng, task.Length)
}

func checkBody(r io.Reader, task *ResponseBodySpec) (bool, error) {
	rng := mt19937.New()
	rng.Seed(task.Seed)
	buf1 := make([]byte, 8192)
	buf2 := make([]byte, 8192)
	var total int64
	for {
		n1, err1 := r.Read(buf1)
		if err1 != nil && err1 != io.EOF {
			return false, err1
		}
		n2, err2 := rng.Read(buf2[:n1])
		if n1 != n2 || err2 != nil {
			panic("RNG failed")
		}
		if bytes.Compare(buf1[:n1], buf2[:n1]) != 0 {
			return false, nil
		}
		total += int64(n1)
		if err1 == io.EOF {
			break
		}
	}
	return total == task.Length, nil
}
