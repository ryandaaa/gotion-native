package notion

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshOnUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(401)
			io.WriteString(w, `{"code":"unauthorized","message":"expired"}`)
			return
		}
		io.WriteString(w, `{"object":"user","id":"bot"}`)
	}))
	defer srv.Close()
	c := New("old")
	c.Base = srv.URL
	var refreshed atomic.Int32
	c.Refresh = func(context.Context) (string, error) { refreshed.Add(1); return "new", nil }
	me, err := c.Me(context.Background())
	if err != nil || me.ID != "bot" || refreshed.Load() != 1 || c.Token != "new" {
		t.Fatalf("me %+v err %v refreshed %d token %q", me, err, refreshed.Load(), c.Token)
	}
}

func TestBucketBurstsThenPaces(t *testing.T) {
	c := New("t")
	start := time.Now()
	for i := 0; i < 8; i++ {
		c.wait(context.Background())
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("a burst of 8 took %v", d)
	}
	c.wait(context.Background())
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("the 9th request went after %v, not paced", d)
	}
}
