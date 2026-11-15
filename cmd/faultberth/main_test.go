package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportCannotOverwriteSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.sqlite")
	if err := os.WriteFile(source, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.sqlite")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{source, alias} {
		if err := run(target, "127.0.0.1:0", source, "", "", ""); err == nil {
			t.Fatalf("allowed source as destination: %s", target)
		}
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("source changed: %q, %v", data, err)
	}
}

func TestLoopbackOnly(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:8080", "192.0.2.1:8080", "localhost:8080"} {
		if err := requireLoopback(address); err == nil {
			t.Fatalf("accepted non-loopback IP %q", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080"} {
		if err := requireLoopback(address); err != nil {
			t.Fatalf("rejected loopback %q: %v", address, err)
		}
	}
}
