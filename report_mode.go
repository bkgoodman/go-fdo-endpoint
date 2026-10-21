// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fido-device-onboard/go-fdo/fsim"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// Report mode: re-run TO2 (credential reuse) only to return diagnostics to
// the owner as standalone fdo.payload logs (fdo.payload.md "Standalone
// Diagnostics"). Every payload the owner offers is rejected before any data
// is sent, and no other FSIM is offered. Typical use is an installer hook
// (late-commands, error-commands, %post, %onerror) reporting the outcome of
// an install that ran after the normal endpoint run staged its config.

// defaultReportMaxSize caps each report file. Installer logs are larger than
// the 64 KiB handler-diagnostics cap; the whole report is buffered in memory.
const defaultReportMaxSize = 1 << 20

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// reportOptions holds the -report* command-line flags.
type reportOptions struct {
	Files          stringList
	Phase          string
	Outcome        int // -1 = not stated
	Message        string
	PayloadName    string
	PayloadVersion string
	MaxSize        int
}

var report = reportOptions{Outcome: -1, MaxSize: defaultReportMaxSize}

// enabled reports whether this run is a report-only session.
func (r *reportOptions) enabled() bool { return len(r.Files) > 0 }

// logs reads the report files into standalone logs, one per file. A file
// that cannot be read is noted in the report rather than failing the run:
// an error hook still needs to tell the owner that something went wrong.
func (r *reportOptions) logs() []*fsim.StandaloneLog {
	var logs []*fsim.StandaloneLog
	var missing []string
	for _, path := range r.Files {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			fmt.Printf("[📋 REPORT] Cannot read %s: %v\n", path, err)
			missing = append(missing, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		logs = append(logs, r.newLog(data, filepath.Base(path)))
	}
	if len(missing) > 0 {
		note := "report files not readable on the device:\n" + strings.Join(missing, "\n") + "\n"
		logs = append(logs, r.newLog([]byte(note), "fdo-endpoint"))
	}
	return logs
}

func (r *reportOptions) newLog(data []byte, source string) *fsim.StandaloneLog {
	l := &fsim.StandaloneLog{
		PayloadLog:     fsim.PayloadLog{Data: data, Source: source},
		Phase:          r.Phase,
		OutcomeMessage: r.Message,
		PayloadName:    r.PayloadName,
		PayloadVersion: r.PayloadVersion,
	}
	if r.Outcome >= 0 {
		outcome := r.Outcome
		l.Outcome = &outcome
	}
	return l
}

// modules returns the device modules for a report-only session and the
// payload module, whose StandaloneLogs record what the owner did with each.
func (r *reportOptions) modules() (map[string]serviceinfo.DeviceModule, *fsim.Payload) {
	payload := &fsim.Payload{
		ReportOnly:     true,
		StandaloneLogs: r.logs(),
		MaxLogSize:     r.MaxSize,
		Active:         true,
	}
	fmt.Printf("[📋 REPORT] Report-only session: %d log(s), phase=%q, outcome=%d\n",
		len(payload.StandaloneLogs), r.Phase, r.Outcome)
	return map[string]serviceinfo.DeviceModule{
		"fido_alliance": &fsim.Interop{},
		"fdo.payload":   payload,
	}, payload
}

// checkDelivered prints the owner's answer for each log and returns an error
// unless all of them were retained, so a hook can retry or fall back.
func checkDelivered(payload *fsim.Payload) error {
	var undelivered int
	for i, l := range payload.StandaloneLogs {
		switch {
		case l.Delivered():
			fmt.Printf("[📋 REPORT] Log %d (%s) delivered\n", i, l.Source)
			continue
		case l.Declined != nil:
			fmt.Printf("[📋 REPORT] Log %d (%s) declined by owner: reason=%d %s\n", i, l.Source, l.Declined.ReasonCode, l.Declined.Message)
		case l.Result != nil:
			fmt.Printf("[📋 REPORT] Log %d (%s) not retained by owner: %d %s\n", i, l.Source, l.Result.StatusCode, l.Result.Message)
		default:
			fmt.Printf("[📋 REPORT] Log %d (%s) not sent: owner did not activate fdo.payload\n", i, l.Source)
		}
		undelivered++
	}
	if undelivered > 0 {
		return fmt.Errorf("%d of %d report log(s) not delivered", undelivered, len(payload.StandaloneLogs))
	}
	return nil
}
