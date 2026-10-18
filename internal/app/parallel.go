package app

import "sync"

func parallel(jobs, n int, fn func(i int)) {
	sem := make(chan struct{}, max(jobs, 1))
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { runLimited(sem, i, fn) })
	}
	wg.Wait()
}

func runLimited(sem chan struct{}, i int, fn func(i int)) {
	sem <- struct{}{}
	defer func() { <-sem }()

	fn(i)
}
