package app

import "sync"

func parallel(jobs, n int, fn func(i int)) {
	sem := make(chan struct{}, max(jobs, 1))
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}
