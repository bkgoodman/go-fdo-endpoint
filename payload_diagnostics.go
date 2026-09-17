package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/fido-device-onboard/go-fdo/fsim"
)

// defaultDiagnosticCap bounds how much handler output is retained for upload.
// Anything beyond this is dropped and the log is flagged as truncated.
const defaultDiagnosticCap = 64 * 1024

// DiagnosticCollector accumulates output produced while applying a payload so
// that it can be returned to the onboarding service at the end of the cycle
// via the payload-log-* messages defined in fdo.payload.md.
//
// Without this, handler stdout/stderr goes only to the device's console and is
// lost the moment the machine reboots - which is precisely when an operator
// most needs to know why an install failed. payload-result carries only a
// one-line summary bounded by the ServiceInfo MTU.
//
// It implements both io.Writer (so command output can be tee'd into it) and
// fsim.LogProvider (so the FSIM can collect it).
type DiagnosticCollector struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool

	// Cap bounds retained bytes. Zero selects defaultDiagnosticCap.
	Cap int

	// Source labels the origin of the output, e.g. "handler".
	Source string

	// SendOnSuccess uploads diagnostics even when the payload applied
	// cleanly. Off by default: successful onboards are the common case and
	// there is no reason to pay for the transfer.
	SendOnSuccess bool
}

var _ fsim.LogProvider = (*DiagnosticCollector)(nil)

// NewDiagnosticCollector returns a collector labelled with the given source.
func NewDiagnosticCollector(source string) *DiagnosticCollector {
	return &DiagnosticCollector{Cap: defaultDiagnosticCap, Source: source}
}

// Write implements io.Writer, retaining output up to Cap bytes. It always
// reports the full length as written so that tee-ing into it never causes the
// producing command to see a short write.
func (d *DiagnosticCollector) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	limit := d.Cap
	if limit <= 0 {
		limit = defaultDiagnosticCap
	}

	if remaining := limit - d.buf.Len(); remaining > 0 {
		if len(p) <= remaining {
			d.buf.Write(p)
		} else {
			d.buf.Write(p[:remaining])
			d.truncated = true
		}
	} else if len(p) > 0 {
		d.truncated = true
	}

	return len(p), nil
}

// Printf appends a formatted line to the diagnostics without echoing it.
func (d *DiagnosticCollector) Printf(format string, args ...any) {
	_, _ = fmt.Fprintf(d, format, args...)
}

// Reset discards accumulated output, readying the collector for the next
// payload.
func (d *DiagnosticCollector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buf.Reset()
	d.truncated = false
}

// Len returns the number of bytes currently retained.
func (d *DiagnosticCollector) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buf.Len()
}

// PayloadLog implements fsim.LogProvider. It is called once per payload, after
// the handler has run and immediately before payload-result is sent. The
// collector is reset afterwards so each payload reports only its own output.
func (d *DiagnosticCollector) PayloadLog(_ context.Context, mimeType, name string, statusCode int) (*fsim.PayloadLog, error) {
	d.mu.Lock()
	data := append([]byte(nil), d.buf.Bytes()...)
	truncated := d.truncated
	d.buf.Reset()
	d.truncated = false
	d.mu.Unlock()

	if len(data) == 0 {
		return nil, nil
	}
	if statusCode == 0 && !d.SendOnSuccess {
		fmt.Printf("[📋 DIAGNOSTICS] Suppressing %d bytes of output for successful payload %s\n",
			len(data), name)
		return nil, nil
	}

	fmt.Printf("[📋 DIAGNOSTICS] Offering %d bytes of output to owner (mime=%s, name=%s, status=%d, truncated=%v)\n",
		len(data), mimeType, name, statusCode, truncated)

	return &fsim.PayloadLog{
		Data:        data,
		ContentType: "text/plain",
		Truncated:   truncated,
		Source:      d.Source,
	}, nil
}
