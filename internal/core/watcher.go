package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Adapter struct {
	ProgramID, Asset                                 string
	RejectedProgram, RejectedSecret, RejectedInvalid int
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
	Library   *Library
	Recommend func([]Evidence) (Board, error)
	Sources   []string
	BoardPath string
	Adapter   Adapter
	Events    []Evidence
	states    map[string]fileState
}

func (w *Watcher) Poll() (int, error) {
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
		_, err = f.Seek(st.Offset, 0)
		if err != nil {
			f.Close()
			return count, err
		}
		// Process complete lines only, so a concurrently appended JSON event is not lost.
		buf := make([]byte, info.Size()-st.Offset)
		_, err = f.ReadAt(buf, st.Offset)
		f.Close()
		if err != nil && len(buf) > 0 {
			return count, err
		}
		end := bytes.LastIndexByte(buf, '\n')
		if end < 0 {
			continue
		}
		for _, line := range strings.Split(string(buf[:end]), "\n") {
			st.Line++
			e := w.Adapter.Event(line, fmt.Sprintf("%s:%d", filepath.Base(path), st.Line))
			if e != nil {
				w.Events = append(w.Events, *e)
				count++
			}
		}
		st.Offset += int64(end + 1)
		st.Info = info
		w.states[path] = st
	}
	if count > 0 {
		var board Board
		if w.Recommend != nil {
			var err error
			board, err = w.Recommend(w.Events)
			if err != nil {
				return count, err
			}
		} else {
			board = w.Library.Recommend(w.Events, 0)
		}
		if err := os.MkdirAll(filepath.Dir(w.BoardPath), 0700); err != nil {
			return count, err
		}
		if err := os.WriteFile(w.BoardPath, []byte("# Atlas — board de melhor caminho\n\n"+board.Board), 0600); err != nil {
			return count, err
		}
	}
	return count, nil
}
