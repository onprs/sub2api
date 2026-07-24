package service

import (
	"bufio"
	"errors"
	"io"
	"testing"
)

type repeatingByteReader byte

func (r repeatingByteReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = byte(r)
	}
	return len(buffer), nil
}

func TestOpenAIStreamingDefaultMaxLineRejectsOversizedRecord(t *testing.T) {
	const wantMaxLineSize = 40 * 1024 * 1024
	if defaultMaxLineSize != wantMaxLineSize {
		t.Fatalf("defaultMaxLineSize = %d, want %d", defaultMaxLineSize, wantMaxLineSize)
	}

	reader := io.LimitReader(repeatingByteReader('a'), int64(defaultMaxLineSize)+1)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), defaultMaxLineSize)

	if scanner.Scan() {
		t.Fatal("scanner unexpectedly accepted an oversized SSE record")
	}
	if !errors.Is(scanner.Err(), bufio.ErrTooLong) {
		t.Fatalf("scanner error = %v, want ErrTooLong", scanner.Err())
	}
}
