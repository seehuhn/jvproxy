package test

import (
	"fmt"
	"testing"
)

func TestSecret(t *testing.T) {
	if len(testerSecret) == 0 {
		t.Fatal("tester secret not initialized")
	}
}

func TestUniquePath(t *testing.T) {
	for i := 10; i < 80; i++ {
		u1 := UniquePath(i)
		if len(u1) != i {
			t.Errorf("len(%q) != %d", u1, i)
		}
		u2 := UniquePath(i)
		if len(u2) != i {
			t.Errorf("len(%q) != %d", u2, i)
		}
		if u1 == u2 {
			t.Errorf("path %q not unique", u1)
		}
		fmt.Println(u1)
		fmt.Println(u2)
	}
}
