package terminal

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// Recorder writes asciicast v2 format events to a writer.
type Recorder struct {
	mu    sync.Mutex
	w     io.WriteCloser
	start time.Time
}

type asciicastHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Env       map[string]string `json:"env,omitempty"`
}

// NewRecorder creates a recorder that writes asciicast v2 to w.
func NewRecorder(w io.WriteCloser, cols, rows int) (*Recorder, error) {
	r := &Recorder{
		w:     w,
		start: time.Now(),
	}
	header := asciicastHeader{
		Version:   2,
		Width:     cols,
		Height:    rows,
		Timestamp: r.start.Unix(),
	}
	data, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("marshal header: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	return r, nil
}

// Output records a PTY output event.
func (r *Recorder) Output(data []byte) {
	r.writeEvent("o", data)
}

// Input records a user/agent input event.
func (r *Recorder) Input(data []byte) {
	r.writeEvent("i", data)
}

func (r *Recorder) writeEvent(eventType string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := time.Since(r.start).Seconds()
	// Format: [elapsed, type, data]
	line := fmt.Sprintf("[%f, %q, %s]\n", elapsed, eventType, jsonString(data))
	_, _ = r.w.Write([]byte(line))
}

// Close flushes and closes the underlying writer.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.w.Close()
}

func jsonString(data []byte) string {
	b, _ := json.Marshal(string(data))
	return string(b)
}
