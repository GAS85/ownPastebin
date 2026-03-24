package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentPasteCreation(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	const n = 50
	var wg sync.WaitGroup
	errors := make(chan string, n)

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf("concurrent paste %d", i)
			res := doRequest(t, handler, "POST", "/", strings.NewReader(body))
			if res.Code != 201 {
				errors <- fmt.Sprintf("goroutine %d: expected 201, got %d — %s", i, res.Code, res.Body.String())
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestConcurrentBurnAfterRead(t *testing.T) {
	// Only one of N concurrent readers should succeed — all others get 404.
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?burn=true", strings.NewReader("burn race"))
	id := extractID(t, res.Body.String())

	const n = 20
	var wg sync.WaitGroup
	successes := make(chan int, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := doRequest(t, handler, "GET", "/"+id, nil)
			if r.Code == 200 {
				successes <- 1
			}
		}()
	}

	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Fatalf("burn-after-read: expected exactly 1 successful read, got %d", count)
	}
}

func TestConcurrentStorageAccess(t *testing.T) {
	// Hammer the SQLite storage directly with concurrent reads and writes.
	s := newTestStorage(t)

	const writers = 20
	const readers = 20
	var wg sync.WaitGroup
	errors := make(chan string, writers+readers)

	// Writers
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-%d", i)
			if err := s.Save(key, &PasteData{Content: "data"}, 0); err != nil {
				errors <- fmt.Sprintf("Save %s: %v", key, err)
			}
		}(i)
	}

	// Readers (some keys may not exist yet — that's fine, just must not panic)
	for i := range readers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-%d", i%writers)
			_, err := s.Get(key)
			if err != nil {
				errors <- fmt.Sprintf("Get %s: %v", key, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}
