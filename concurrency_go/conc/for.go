package conc

import "sync"

func For(inputs <-chan Box, numWorkers int, foo func(i Box)) (wait func()) {
	m := new(sync.Mutex)

	block := make(chan bool, numWorkers)
	for j := 0; j < numWorkers; j++ {
		go func() {
			for {
				m.Lock()
				i, done := <-inputs, closed(inputs)
				m.Unlock()
				if done {
					break
				}
				foo(i)
			}
			block <- true
		}()
	}
	wait = func() {
		for i := 0; i < numWorkers; i++ {
			<-block
		}
	}
	return wait
}
