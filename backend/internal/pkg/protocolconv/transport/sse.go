package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const DefaultMaxSSERecordBytes = 4 << 20

// ErrSSEDone is returned when a complete SSE record contains the terminal
// [DONE] sentinel. Callers should stop reading and finalize protocol state.
var ErrSSEDone = errors.New("SSE stream done")

// SSERecordTooLargeError reports the configured record bound that was exceeded.
type SSERecordTooLargeError struct {
	MaxBytes int
}

func (e *SSERecordTooLargeError) Error() string {
	if e == nil {
		return "SSE record exceeds configured limit"
	}
	return fmt.Sprintf("SSE record exceeds %d bytes", e.MaxBytes)
}

// SSERecord is one parsed SSE record. Data is the JSON payload formed by
// joining multiple data fields with a newline, as required by the SSE spec.
type SSERecord struct {
	Event    string
	ID       string
	Retry    string
	Data     []byte
	Metadata map[string]any
}

// SSEProgress is a transport-only snapshot for timeout and keepalive policy.
// InRecord reports whether at least one non-empty SSE line has been read
// without its terminating blank line.
type SSEProgress struct {
	LastReadAt time.Time
	InRecord   bool
}

// SSEParser reads complete SSE records from one upstream body. It owns the
// reader and must not be shared between requests.
type SSEParser struct {
	reader    *bufio.Reader
	closer    io.Closer
	maxRecord int

	mu         sync.Mutex
	closed     bool
	done       bool
	lastReadAt time.Time
	inRecord   bool
}

// NewSSEParser creates a bounded request-scoped parser. A non-positive maximum
// uses DefaultMaxSSERecordBytes.
func NewSSEParser(body io.ReadCloser, maxRecordBytes int) *SSEParser {
	if maxRecordBytes <= 0 {
		maxRecordBytes = DefaultMaxSSERecordBytes
	}
	return &SSEParser{
		reader:     bufio.NewReaderSize(body, 64<<10),
		closer:     body,
		maxRecord:  maxRecordBytes,
		lastReadAt: time.Now(),
	}
}

// Progress returns a race-safe parser activity snapshot. It does not consume
// input and is intended only for transport timeout and keepalive decisions.
func (p *SSEParser) Progress() SSEProgress {
	if p == nil {
		return SSEProgress{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return SSEProgress{LastReadAt: p.lastReadAt, InRecord: p.inRecord}
}

// Next returns the next data-bearing record. Comments and field-only records
// are ignored. The payload must be valid JSON unless it is [DONE].
func (p *SSEParser) Next(ctx context.Context) (SSERecord, error) {
	for {
		record, err := p.NextRecord(ctx)
		if err != nil {
			return SSERecord{}, err
		}
		if len(record.Data) == 0 {
			continue
		}
		if !json.Valid(record.Data) {
			return SSERecord{}, errors.New("malformed SSE JSON payload")
		}
		return record, nil
	}
}

// NextRecord returns the next bounded SSE record without interpreting its data.
// It preserves field-only error records and non-JSON payloads for service-owned
// transport policy. The [DONE] sentinel remains a parser terminal except when
// an explicit error event must be returned to service policy first.
func (p *SSEParser) NextRecord(ctx context.Context) (SSERecord, error) {
	if p == nil || p.reader == nil {
		return SSERecord{}, errors.New("nil SSE parser")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return SSERecord{}, io.ErrClosedPipe
	}
	if p.done {
		p.mu.Unlock()
		return SSERecord{}, ErrSSEDone
	}
	p.mu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			return SSERecord{}, err
		}
		type readResult struct {
			record SSERecord
			eof    bool
			err    error
		}
		resultCh := make(chan readResult, 1)
		go func() {
			record, eof, err := p.readRecord(context.Background())
			resultCh <- readResult{record: record, eof: eof, err: err}
		}()
		var result readResult
		select {
		case <-ctx.Done():
			_ = p.Close()
			return SSERecord{}, ctx.Err()
		case result = <-resultCh:
		}
		record, eof, err := result.record, result.eof, result.err
		if err != nil {
			return SSERecord{}, err
		}
		if len(record.Data) == 0 && record.Event == "" && record.ID == "" && record.Retry == "" {
			if eof {
				return SSERecord{}, io.EOF
			}
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record.Event), "error") && bytes.Equal(bytes.TrimSpace(record.Data), []byte("[DONE]")) {
			p.mu.Lock()
			p.done = true
			p.mu.Unlock()
			return SSERecord{}, ErrSSEDone
		}
		return record, nil
	}
}

// Close deterministically releases the upstream body.
func (p *SSEParser) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	closer := p.closer
	p.mu.Unlock()
	if closer != nil {
		return closer.Close()
	}
	return nil
}

func (p *SSEParser) readRecord(ctx context.Context) (SSERecord, bool, error) {
	var record SSERecord
	var dataLines [][]byte
	size := 0
	sawField := false

	for {
		if err := ctx.Err(); err != nil {
			return SSERecord{}, false, err
		}
		line, err := p.readLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return SSERecord{}, false, err
		}
		eof := errors.Is(err, io.EOF)
		if len(line) > 0 || !eof {
			p.recordReadProgress(len(line) > 0)
		}
		size += len(line) + 1
		if size > p.maxRecord {
			return SSERecord{}, false, &SSERecordTooLargeError{MaxBytes: p.maxRecord}
		}

		if len(line) == 0 {
			if sawField || eof {
				record.Data = bytes.Join(dataLines, []byte{'\n'})
				return record, eof, nil
			}
			if eof {
				return SSERecord{}, true, nil
			}
			continue
		}

		if line[0] != ':' {
			sawField = true
			field, value := splitSSEField(line)
			switch field {
			case "event":
				record.Event = string(value)
			case "data":
				dataLines = append(dataLines, append([]byte(nil), value...))
			case "id":
				record.ID = string(value)
			case "retry":
				record.Retry = string(value)
			}
		}

		if eof {
			record.Data = bytes.Join(dataLines, []byte{'\n'})
			return record, true, nil
		}
	}
}

func (p *SSEParser) recordReadProgress(inRecord bool) {
	p.mu.Lock()
	p.lastReadAt = time.Now()
	p.inRecord = inRecord
	p.mu.Unlock()
}

func (p *SSEParser) readLine() ([]byte, error) {
	var line []byte
	for {
		part, prefix, err := p.reader.ReadLine()
		line = append(line, part...)
		if len(line) > p.maxRecord {
			return nil, &SSERecordTooLargeError{MaxBytes: p.maxRecord}
		}
		if err != nil {
			return trimTrailingCR(line), err
		}
		if !prefix {
			return trimTrailingCR(line), nil
		}
	}
}

func splitSSEField(line []byte) (string, []byte) {
	index := bytes.IndexByte(line, ':')
	if index < 0 {
		return string(line), nil
	}
	value := line[index+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return string(line[:index]), value
}

func trimTrailingCR(line []byte) []byte {
	return bytes.TrimSuffix(line, []byte{'\r'})
}

// JSONErrorPreview returns a bounded, printable payload description for logs.
// It intentionally does not expose the complete request or response body.
func JSONErrorPreview(data []byte, limit int) string {
	if limit <= 0 {
		limit = 256
	}
	value := strings.TrimSpace(string(data))
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}
