package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestGraphServeImpliesHTMLFormat(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize floop store
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Use io.Pipe so we can read server output without race conditions.
	// If --serve is honored, the server blocks and writes "Graph server running at ...".
	// If --serve is ignored, DOT text is printed and the command returns.
	pr, pw := io.Pipe()

	go func() {
		rootCmd2 := newTestRootCmd()
		rootCmd2.AddCommand(newGraphCmd())
		rootCmd2.SetOut(pw)
		rootCmd2.SetArgs([]string{"graph", "--serve", "--no-open", "--root", tmpDir})
		rootCmd2.Execute()
		pw.Close()
	}()

	// Read the first chunk of output. This blocks until the server writes
	// its startup message or the command completes with DOT output.
	type readResult struct {
		data string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := pr.Read(buf)
		ch <- readResult{string(buf[:n]), err}
	}()

	select {
	case r := <-ch:
		if r.err != nil && r.err != io.EOF {
			t.Fatalf("read error: %v", r.err)
		}
		if strings.Contains(r.data, "digraph") {
			t.Fatalf("--serve was ignored: got raw DOT output instead of starting server: %s", r.data)
		}
		if !strings.Contains(r.data, "Graph server running at") {
			t.Fatalf("expected 'Graph server running at', got: %q", r.data)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server output")
	}

	// Close the pipe reader to unblock the server goroutine.
	pr.Close()
}

func TestGraphDefaultFormatIsDOT(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize floop store
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Run graph without --serve — should produce DOT output
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newGraphCmd())
	var out bytes.Buffer
	rootCmd2.SetOut(&out)
	rootCmd2.SetArgs([]string{"graph", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("graph failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "digraph") {
		t.Errorf("expected DOT output containing 'digraph', got: %s", output)
	}
}
