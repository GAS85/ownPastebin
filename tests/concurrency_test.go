package main_test

import (
	"strings"
	"sync"
	"testing"
	"github.com/GAS85/ownPastebin"
)

func TestConcurrentRequests(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res := doRequest(handler, "POST", "/", strings.NewReader("data"))
			if res.Code != 201 {
				t.Errorf("failed request: %d", res.Code)
			}
		}(i)
	}

	wg.Wait()
}