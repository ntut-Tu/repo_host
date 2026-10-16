package cache

import (
	"testing"
	"time"
)

func TestPersistentCacheExpires(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put("issue", "7", "value", time.Hour); err != nil {
		t.Fatal(err)
	}
	second, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if !second.Get("issue", "7", &result) || result != "value" {
		t.Fatal("persisted value missing")
	}
	if err := second.Put("issue", "8", "expired", -time.Second); err != nil {
		t.Fatal(err)
	}
	if second.Get("issue", "8", &result) {
		t.Fatal("expired value was returned")
	}
}
