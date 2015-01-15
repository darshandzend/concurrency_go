package conc

import (
	"testing"
)

func TestFold(t *testing.T) {
	sum := func(a Box, b Box) Box {
		return a.(int) + b.(int)
	}
	numbers := make(chan Box)
	go func() {
		for i := 0; i < 10; i++ {
			numbers <- i
		}
		close(numbers)
	}()
	totalSum := Fold(numbers, sum)

	if totalSum.(int) != 45 {
		t.Fail()
	}
}
