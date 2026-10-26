package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxPollBytes bounds how much a single poll reads from one source. A producer
// that appends a large burst must not force an allocation the size of the
// burst; the remainder is picked up by the next poll.
const maxPollBytes = 1 << 20

// maxSessionEvents bounds the accumulated session window. Events accumulate on
// purpose, because conditions are evaluated over the whole flow, but an
// unbounded window would grow past the remote MCP body and evidence limits and
// resend the entire session on every poll.
const maxSessionEvents = 2000

type Adapter struct {
	ProgramID, Asset                                 string
	RejectedProgram, RejectedSecret, RejectedInvalid int
}

// Refusals reports why input was dropped. Counts only: refused content is
// never logged or included in an error.
type Refusals struct {
	Program, Secret, Invalid, Window int
}

func (a *Adapter) Event(line, ref string) *Evidence {
	s := strings.TrimSpace(line)
	if s == "" {
		return nil
	}
	if HasSecret(s) {
		a.RejectedSecret++
		return nil
	}
	if strings.Contains(s, "hive_burp:") {
		a.RejectedInvalid++
		return nil
	}
	e := Evidence{Kind: "nota", Value: s, ProgramID: a.ProgramID, Asset: a.Asset, Ref: ref}
	if strings.HasPrefix(s, "{") {
		e = Evidence{}
		if json.Unmarshal([]byte(s), &e) != nil {
			a.RejectedInvalid++
			return nil
		}
		// Missing scope may be filled, but explicit scope must match this watcher.
		if e.ProgramID != "" && a.ProgramID != "" && e.ProgramID != a.ProgramID {
			a.RejectedProgram++
			return nil
		}
		if e.ProgramID == "" {
			e.ProgramID = a.ProgramID
		}
		if e.Asset == "" {
			e.Asset = a.Asset
		}
		if e.Ref == "" {
			e.Ref = ref
		}
	}
	if e.Validate() != nil {
		a.RejectedInvalid++
		return nil
	}
	return &e
}

type fileState struct {
	Offset int64
	Line   int
	Info   os.FileInfo
}
type Watcher struct {
	Library      *Library
	Recommend    func([]Evidence) (Board, error)
	Sources      []string
	BoardPath    string
	ManifestPath string
	Adapter      Adapter
	Events       []Evidence
	// MaxEvents bounds the session window and MaxPollBytes one poll's read;
	// both fall back to the package defaults when zero.
	MaxEvents     int
	MaxPollBytes  int
	dropped       int
	states        map[string]fileState
	burpIdentity  string
	burpSource    string
	seenBurpIDs   map[string]struct{}
	burpUsedFiles map[string]string
	burpRevoked   map[string]bool
}

// Refusals exposes the running totals so a caller can report them.
func (w *Watcher) Refusals() Refusals {
	return Refusals{w.Adapter.RejectedProgram, w.Adapter.RejectedSecret, w.Adapter.RejectedInvalid, w.dropped}
}

func (w *Watcher) add(e Evidence) {
	limit := w.MaxEvents
	if limit <= 0 {
		limit = maxSessionEvents
	}
	w.Events = append(w.Events, e)
	if extra := len(w.Events) - limit; extra > 0 {
		w.Events = append(w.Events[:0], w.Events[extra:]...)
		w.dropped += extra
	}
}

func (w *Watcher) Poll() (int, error) {
	if w.ManifestPath != "" {
		return w.pollBurp()
	}
	if w.states == nil {
		w.states = map[string]fileState{}
	}
	count := 0
	for _, path := range w.Sources {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return count, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return count, err
		}
		st := w.states[path]
		if st.Info != nil && (!os.SameFile(st.Info, info) || info.Size() < st.Offset) {
			st = fileState{}
		}
		var buf []byte
		chunk := int64(w.MaxPollBytes)
		if chunk <= 0 {
			chunk = maxPollBytes
		}
		if size := info.Size() - st.Offset; size > 0 {
			if size > chunk {
				size = chunk
			}
			buf = make([]byte, size)
			n, err := f.ReadAt(buf, st.Offset)
			// A concurrent rotation or truncation shortens the read. Keep the
			// bytes that did arrive instead of ending the watch.
			if n == 0 && err != nil && !errors.Is(err, io.EOF) {
				f.Close()
				return count, err
			}
			buf = buf[:n]
		}
		f.Close()
		st.Info = info
		// Process complete lines only, so a concurrently appended JSON event is not lost.
		if end := bytes.LastIndexByte(buf, '\n'); end >= 0 {
			for _, line := range strings.Split(string(buf[:end]), "\n") {
				st.Line++
				e := w.Adapter.Event(line, fmt.Sprintf("%s:%d", filepath.Base(path), st.Line))
				if e != nil {
					w.add(*e)
					count++
				}
			}
			st.Offset += int64(end + 1)
		}
		w.states[path] = st
	}
	if count > 0 {
		if err := w.writeBoard(); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (w *Watcher) writeBoard() error {
	var board Board
	if w.Recommend != nil {
		var err error
		board, err = w.Recommend(w.Events)
		if err != nil {
			return err
		}
	} else {
		board = w.Library.Recommend(w.Events, 0)
	}
	if err := os.MkdirAll(filepath.Dir(w.BoardPath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(w.BoardPath, []byte("# Atlas — board de melhor caminho\n\n"+board.Board), 0600); err != nil {
		return err
	}
	return nil
}
