package main

import (
	"bufio"
	"context"
	"os"
	"path"
	"reflect"
	"testing"
)

func TestFoo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	defer os.RemoveAll(tmpDir)
	if err != nil {
		t.Fatalf("failed to create temporary directory: %+v", err)
	}

	filePath := path.Join(tmpDir, "test.log")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("failed to create log test file: %+v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lines, err := parseMCLog(ctx, filePath)
	if err != nil {
		t.Fatalf("failed to create fooer: %+v", err)
	}

	in := []string{
		"Foo",
		"[01:51:51] [Server thread/INFO]: foobar joined the game",
		"[01:55:54] [Server thread/INFO]: baz left the game",
		"[19:40:33] [Server thread/INFO]: <Foobar> Hello, how are you doing?",
		"Foo",
	}

	out := []any{
		logJoin{user: "foobar"},
		logPart{user: "baz"},
		logMsg{user: "Foobar", msg: "Hello, how are you doing?"},
	}

	go func() {
		writer := bufio.NewWriter(file)
		for _, line := range in {
			writer.WriteString(line)
			writer.WriteString("\n")
		}
		writer.WriteString("EOF for testing\n")
		writer.Flush()
	}()

	for _, expected := range out {
		got := <-lines
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("Expected %T %+v, got %T %+v", expected, expected, got, got)
		}
	}

	for line := range lines {
		t.Logf("Extra outputs that was never parsed: %+v", line)
	}
}
