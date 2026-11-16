//go:build live

package main

import (
	"context"
	"net/http"
	"os"
	"testing"
)

func TestStateLive(t *testing.T) {
	cfg := Config{StateBucket: os.Getenv("STATE_BUCKET")}
	c := &http.Client{}
	if err := markSent(context.Background(), c, cfg, "2026-11-15"); err != nil {
		t.Fatal(err)
	}
	got, err := lastSent(context.Background(), c, cfg)
	if err != nil || got != "2026-11-15" {
		t.Fatalf("got %q, %v", got, err)
	}
}
