// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/fido-device-onboard/go-fdo/fsim"
	"github.com/fido-device-onboard/go-fdo/fsim/chunking"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReportEnabled(t *testing.T) {
	if (&reportOptions{}).enabled() {
		t.Error("enabled with no -report files")
	}
	if !(&reportOptions{Files: stringList{"x"}}).enabled() {
		t.Error("not enabled with a -report file")
	}
}

func TestReportFlagRepeats(t *testing.T) {
	var s stringList
	for _, v := range []string{"a.log", "b.log"} {
		if err := s.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if len(s) != 2 || s.String() != "a.log,b.log" {
		t.Errorf("stringList = %v (%q)", s, s.String())
	}
}

// TestReportLogsCarryOptions: every file becomes one log carrying the
// command-line metadata; Source is the file's base name, not its path.
func TestReportLogsCarryOptions(t *testing.T) {
	dir := t.TempDir()
	r := reportOptions{
		Files:          stringList{writeFile(t, dir, "curtin.log", "curtin ok\n"), writeFile(t, dir, "journal.log", "boot\n")},
		Phase:          "install",
		Outcome:        2,
		Message:        "install failed",
		PayloadName:    "autoinstall.yaml",
		PayloadVersion: "v1",
	}

	logs := r.logs()
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
	for i, want := range []struct{ source, data string }{{"curtin.log", "curtin ok\n"}, {"journal.log", "boot\n"}} {
		l := logs[i]
		if l.Source != want.source || string(l.Data) != want.data {
			t.Errorf("log %d: source=%q data=%q, want %q %q", i, l.Source, l.Data, want.source, want.data)
		}
		if l.Phase != "install" || l.OutcomeMessage != "install failed" ||
			l.PayloadName != "autoinstall.yaml" || l.PayloadVersion != "v1" {
			t.Errorf("log %d metadata = %+v", i, l)
		}
		if l.Outcome == nil || *l.Outcome != 2 {
			t.Errorf("log %d outcome = %v, want 2", i, l.Outcome)
		}
	}
	// Each log gets its own outcome value, not a shared pointer.
	if logs[0].Outcome == logs[1].Outcome {
		t.Error("logs share one Outcome pointer")
	}
}

// TestReportLogsOutcome: -1 is "not stated" (nil); 0 is an explicit success.
func TestReportLogsOutcome(t *testing.T) {
	path := writeFile(t, t.TempDir(), "a.log", "x")
	for _, tc := range []struct {
		outcome int
		want    *int
	}{{-1, nil}, {0, intPtr(0)}, {1, intPtr(1)}} {
		l := (&reportOptions{Files: stringList{path}, Outcome: tc.outcome}).logs()[0]
		switch {
		case tc.want == nil && l.Outcome != nil:
			t.Errorf("outcome %d: got %d, want not stated", tc.outcome, *l.Outcome)
		case tc.want != nil && (l.Outcome == nil || *l.Outcome != *tc.want):
			t.Errorf("outcome %d: got %v", tc.outcome, l.Outcome)
		}
	}
}

func intPtr(v int) *int { return &v }

// TestReportLogsMissingFileIsNoted: an unreadable file must not stop the
// report (an error hook still has to reach the owner); it is listed in an
// extra note log instead.
func TestReportLogsMissingFileIsNoted(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "crash.log")

	logs := (&reportOptions{
		Files:   stringList{writeFile(t, dir, "ok.log", "fine"), missing},
		Outcome: 2,
	}).logs()
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want the readable one plus a note", len(logs))
	}
	note := logs[1]
	if note.Source != "fdo-endpoint" || !strings.Contains(string(note.Data), missing) {
		t.Errorf("note = source %q, data %q", note.Source, note.Data)
	}
	if note.Outcome == nil || *note.Outcome != 2 {
		t.Errorf("note outcome = %v, want the report's outcome", note.Outcome)
	}

	only := (&reportOptions{Files: stringList{missing}, Outcome: -1}).logs()
	if len(only) != 1 || only[0].Source != "fdo-endpoint" {
		t.Errorf("all files missing: got %d logs %+v, want just the note", len(only), only)
	}
}

// TestReportModulesOfferOnlyPayload: a report-only session offers no FSIM
// that could receive or apply anything except the report-only payload module.
func TestReportModulesOfferOnlyPayload(t *testing.T) {
	r := reportOptions{Files: stringList{writeFile(t, t.TempDir(), "a.log", "x")}, MaxSize: 4096}
	modules, payload := r.modules()

	var names []string
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "fdo.payload,fido_alliance" {
		t.Errorf("modules = %v, want only fdo.payload and fido_alliance", names)
	}
	if modules["fdo.payload"] != payload {
		t.Error("returned payload module is not the one registered")
	}
	if !payload.ReportOnly || payload.MaxLogSize != 4096 || len(payload.StandaloneLogs) != 1 {
		t.Errorf("payload module = ReportOnly %v, MaxLogSize %d, %d logs",
			payload.ReportOnly, payload.MaxLogSize, len(payload.StandaloneLogs))
	}
	if payload.UnifiedHandler != nil || payload.ChunkedHandler != nil {
		t.Error("report-only payload module has a payload handler")
	}
}

func TestCheckDelivered(t *testing.T) {
	delivered := func() *fsim.StandaloneLog {
		return &fsim.StandaloneLog{Result: &chunking.ResultMessage{StatusCode: 0}}
	}
	for name, tc := range map[string]struct {
		logs    []*fsim.StandaloneLog
		wantErr string
	}{
		"all delivered": {logs: []*fsim.StandaloneLog{delivered(), delivered()}},
		"declined": {
			logs:    []*fsim.StandaloneLog{delivered(), {Declined: &chunking.AckMessage{ReasonCode: 5}}},
			wantErr: "1 of 2",
		},
		"not retained": {
			logs:    []*fsim.StandaloneLog{{Result: &chunking.ResultMessage{StatusCode: 2}}},
			wantErr: "1 of 1",
		},
		"never sent": {logs: []*fsim.StandaloneLog{{}, {}}, wantErr: "2 of 2"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkDelivered(&fsim.Payload{StandaloneLogs: tc.logs})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// exchange drives an owner module against the report-mode payload module in
// memory, the way the TO2 ServiceInfo loop does: "active" is answered by the
// loop and Yield is called after each owner round. It returns the message
// names the owner sent.
func exchange(t *testing.T, owner *fsim.PayloadOwner, device *fsim.Payload) []string {
	t.Helper()
	ctx := context.WithValue(context.Background(), serviceinfo.MTUKey{}, uint16(1300))
	active := false
	var ownerMsgs []string

	for round := 0; round < 500; round++ {
		producer := serviceinfo.NewProducer("fdo.payload", 1300)
		_, done, err := owner.ProduceInfo(ctx, producer)
		if err != nil {
			t.Fatalf("round %d: owner: %v", round, err)
		}
		var responses []*serviceinfo.KV
		respond := func(name string) io.Writer {
			kv := &serviceinfo.KV{Key: name}
			responses = append(responses, kv)
			return kvWriter{kv}
		}
		for _, kv := range producer.ServiceInfo() {
			_, name, _ := strings.Cut(kv.Key, ":")
			ownerMsgs = append(ownerMsgs, name)
			if name == "active" {
				if !active {
					active = true
					if err := device.Transition(true); err != nil {
						t.Fatal(err)
					}
					_, _ = respond("active").Write([]byte{0xf5})
				}
				continue
			}
			if err := device.Receive(ctx, name, bytes.NewReader(kv.Val), respond, func() {}); err != nil {
				t.Fatalf("round %d: device Receive(%s): %v", round, name, err)
			}
		}
		if active {
			if err := device.Yield(ctx, respond, func() {}); err != nil {
				t.Fatalf("round %d: device Yield: %v", round, err)
			}
		}
		if done {
			return ownerMsgs
		}
		for _, kv := range responses {
			if err := owner.HandleInfo(ctx, kv.Key, bytes.NewReader(kv.Val)); err != nil {
				t.Fatalf("round %d: owner HandleInfo(%s): %v", round, kv.Key, err)
			}
		}
	}
	t.Fatal("exchange did not finish")
	return nil
}

type kvWriter struct{ kv *serviceinfo.KV }

func (w kvWriter) Write(p []byte) (int, error) { w.kv.Val = append(w.kv.Val, p...); return len(p), nil }

// collectingHandler is an owner LogHandler that keeps every log.
type collectingHandler struct{ logs []*fsim.PayloadLogInfo }

func (h *collectingHandler) AcceptLog(_, _ string, _ uint64, _ string) (bool, int, string) {
	return true, 0, ""
}

func (h *collectingHandler) HandleLog(_ context.Context, _, _ string, l *fsim.PayloadLogInfo) error {
	h.logs = append(h.logs, l)
	return nil
}

// TestReportModeAgainstOwner runs the modules built by reportOptions against
// go-fdo's owner: the report (larger than one ServiceInfo message) arrives
// intact with its metadata, the owner's payloads are refused before any data
// is sent, and checkDelivered agrees. Negative control: an owner that does
// not collect logs leaves the report undelivered and checkDelivered fails.
func TestReportModeAgainstOwner(t *testing.T) {
	big := strings.Repeat("subiquity: step\n", 4000) // ~64 KB, many messages at MTU 1300
	r := reportOptions{
		Files:   stringList{writeFile(t, t.TempDir(), "installer.log", big)},
		Phase:   "install",
		Outcome: 0,
		Message: "install complete",
		MaxSize: defaultReportMaxSize,
	}

	t.Run("owner collects logs", func(t *testing.T) {
		handler := &collectingHandler{}
		owner := &fsim.PayloadOwner{LogHandler: handler}
		owner.AddPayloadWithAck("application/x-iso9660-image", "ubuntu.iso", bytes.Repeat([]byte{1}, 10000), nil)
		_, payload := r.modules()

		ownerMsgs := exchange(t, owner, payload)

		if err := checkDelivered(payload); err != nil {
			t.Fatalf("checkDelivered: %v", err)
		}
		if len(handler.logs) != 1 {
			t.Fatalf("owner got %d logs, want 1", len(handler.logs))
		}
		got := handler.logs[0]
		if string(got.Data) != big || got.Source != "installer.log" || got.Phase != "install" ||
			got.Outcome == nil || *got.Outcome != 0 || got.OutcomeMessage != "install complete" || !got.Standalone {
			t.Errorf("owner received %d bytes, metadata %+v", len(got.Data), got)
		}
		for _, m := range ownerMsgs {
			if strings.HasPrefix(m, "payload-data-") {
				t.Fatalf("owner sent payload data to a report-only device: %v", ownerMsgs)
			}
		}
	})

	t.Run("owner does not collect logs", func(t *testing.T) {
		_, payload := r.modules()
		exchange(t, &fsim.PayloadOwner{}, payload)
		if err := checkDelivered(payload); err == nil {
			t.Fatal("checkDelivered succeeded although the owner never received the report")
		}
	})
}
