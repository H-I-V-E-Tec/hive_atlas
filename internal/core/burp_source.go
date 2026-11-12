package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxBurpSourceBytes = 32 << 20
const maxBurpManifestBytes = 4096

var burpProgramRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var burpUUIDRE = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var burpRevisionRE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var burpMethodRE = regexp.MustCompile(`^[A-Z]{1,32}$`)
var burpFlowRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var burpRefRE = regexp.MustCompile(`^hive_burp:([a-z0-9][a-z0-9_-]{0,63})/([a-f0-9-]{36})/([a-f0-9-]{36})$`)

type burpManifest struct {
	SchemaVersion int    `json:"schema_version"`
	ProgramID     string `json:"program_id"`
	SessionID     string `json:"session_id"`
	Generation    int    `json:"generation"`
	ScopeRevision string `json:"scope_revision"`
	State         string `json:"state"`
	UpdatedAt     string `json:"updated_at"`
	ExpiresAt     string `json:"expires_at"`
	FileName      string `json:"file_name"`
}

type burpEvent struct {
	SchemaVersion int     `json:"schema_version"`
	EventID       string  `json:"event_id"`
	TS            string  `json:"ts"`
	ProgramID     string  `json:"program_id"`
	Asset         string  `json:"asset"`
	Kind          string  `json:"kind"`
	Endpoint      string  `json:"endpoint"`
	Method        string  `json:"method"`
	Role          *string `json:"role"`
	Flow          *string `json:"flow"`
	Object        *string `json:"object"`
	Value         string  `json:"value"`
	Ref           string  `json:"ref"`
}

// exactObjectFields rejects unknown, absent and repeated keys, as well as
// trailing JSON. It keeps the wire contract independent of Evidence's looser
// legacy fields and omitempty encoding.
func exactObjectFields(data []byte, fields ...string) error {
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	seen := make(map[string]bool, len(fields))
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return errors.New("invalid JSON field")
		}
		name, ok := key.(string)
		if !ok || !allowed[name] || seen[name] {
			return errors.New("unknown or repeated JSON field")
		}
		seen[name] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return errors.New("invalid JSON value")
		}
	}
	if _, err := dec.Token(); err != nil || len(seen) != len(fields) {
		return errors.New("incomplete JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func parseBurpManifest(data []byte, program string, now time.Time) (burpManifest, error) {
	var m burpManifest
	if err := exactObjectFields(data, "schema_version", "program_id", "session_id", "generation", "scope_revision", "state", "updated_at", "expires_at", "file_name"); err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, errors.New("invalid manifest fields")
	}
	updated, err := time.Parse(time.RFC3339Nano, m.UpdatedAt)
	if err != nil {
		return m, errors.New("invalid manifest update time")
	}
	expires, err := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	if err != nil {
		return m, errors.New("invalid manifest expiry")
	}
	if m.SchemaVersion != 1 || !burpProgramRE.MatchString(m.ProgramID) || m.ProgramID != program ||
		!burpUUIDRE.MatchString(m.SessionID) || m.Generation < 1 ||
		!burpRevisionRE.MatchString(m.ScopeRevision) ||
		!validBurpFileName(m.FileName) ||
		m.State != "active" || updated.After(now.Add(5*time.Second)) ||
		now.Sub(updated) > 30*time.Second || !expires.After(now) || expires.Before(updated) {
		return m, errors.New("inactive or invalid Burp source")
	}
	return m, nil
}

func validBurpFileName(name string) bool {
	return strings.HasSuffix(name, ".jsonl") && burpUUIDRE.MatchString(strings.TrimSuffix(name, ".jsonl"))
}

func parseBurpEvent(line []byte, manifest burpManifest) (burpEvent, Evidence, error) {
	var event burpEvent
	var evidence Evidence
	if HasSecret(string(line)) {
		return event, evidence, errors.New("credential in Burp event")
	}
	if err := exactObjectFields(line, "schema_version", "event_id", "ts", "program_id", "asset", "kind", "endpoint", "method", "role", "flow", "object", "value", "ref"); err != nil {
		return event, evidence, err
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return event, evidence, errors.New("invalid Burp event fields")
	}
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(line, &rawFields)
	if bytes.Equal(bytes.TrimSpace(rawFields["value"]), []byte("null")) {
		return event, evidence, errors.New("invalid Burp value")
	}
	ref := burpRefRE.FindStringSubmatch(event.Ref)
	_, timeErr := time.Parse(time.RFC3339Nano, event.TS)
	if event.SchemaVersion != 1 || !burpUUIDRE.MatchString(event.EventID) || timeErr != nil ||
		event.ProgramID != manifest.ProgramID || !burpProgramRE.MatchString(event.ProgramID) ||
		len(event.Asset) < 1 || len(event.Asset) > 253 ||
		(event.Kind != "endpoint" && event.Kind != "param" && event.Kind != "header") ||
		len(event.Endpoint) < 1 || len(event.Endpoint) > 2048 || !strings.HasPrefix(event.Endpoint, "/") ||
		!burpMethodRE.MatchString(event.Method) || event.Object != nil ||
		len(event.Value) > 512 || len(ref) != 4 || ref[1] != manifest.ProgramID ||
		ref[2] != manifest.SessionID || !burpUUIDRE.MatchString(ref[3]) {
		return event, evidence, errors.New("invalid Burp event contract")
	}
	if event.Role != nil && *event.Role != "user" && *event.Role != "admin" && *event.Role != "anon" {
		return event, evidence, errors.New("invalid Burp role")
	}
	if event.Flow != nil && (len(*event.Flow) > 64 || !burpFlowRE.MatchString(*event.Flow)) {
		return event, evidence, errors.New("invalid Burp flow")
	}
	evidence = Evidence{SchemaVersion: 1, Kind: event.Kind, Value: event.Value, ProgramID: event.ProgramID,
		Asset: event.Asset, Endpoint: event.Endpoint, Method: event.Method, Ref: event.Ref}
	if event.Role != nil {
		evidence.Role = *event.Role
	}
	if event.Flow != nil {
		evidence.Flow = *event.Flow
	}
	if err := evidence.Validate(); err != nil {
		return event, Evidence{}, err
	}
	return event, evidence, nil
}

func openPrivateRegular(path string, maxBytes int64) (*os.File, os.FileInfo, error) {
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return nil, nil, errors.New("Burp source directory must be private and regular")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() > maxBytes {
		return nil, nil, errors.New("Burp source must be private, regular and within size limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, nil, errors.New("Burp source changed during open")
	}
	return f, info, nil
}

func (w *Watcher) currentBurpManifest() (burpManifest, string, error) {
	var empty burpManifest
	if !burpProgramRE.MatchString(w.Adapter.ProgramID) || len(w.Sources) != 0 {
		return empty, "", errors.New("Burp watch requires a single manifest and program")
	}
	f, _, err := openPrivateRegular(w.ManifestPath, maxBurpManifestBytes)
	if err != nil {
		return empty, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBurpManifestBytes+1))
	if err != nil || len(data) > maxBurpManifestBytes {
		return empty, "", errors.New("Burp manifest exceeded size limit")
	}
	m, err := parseBurpManifest(data, w.Adapter.ProgramID, time.Now())
	if err != nil {
		return empty, "", err
	}
	return m, filepath.Join(filepath.Dir(w.ManifestPath), m.FileName), nil
}

func (w *Watcher) clearBurp() error {
	if w.burpIdentity != "" {
		if w.burpRevoked == nil {
			w.burpRevoked = map[string]bool{}
		}
		w.burpRevoked[w.burpIdentity] = true
	}
	w.Events = nil
	w.states = nil
	w.seenBurpIDs = nil
	w.burpIdentity = ""
	w.burpSource = ""
	return w.removeBurpBoard()
}

func (w *Watcher) removeBurpBoard() error {
	if w.BoardPath == "" {
		return nil
	}
	if err := os.Remove(w.BoardPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (w *Watcher) pollBurp() (int, error) {
	m, path, err := w.currentBurpManifest()
	if err != nil {
		return 0, errors.Join(err, w.clearBurp())
	}
	identity := fmt.Sprintf("%s/%d/%s/%s", m.SessionID, m.Generation, m.ScopeRevision, m.FileName)
	if w.burpRevoked[identity] || w.burpUsedFiles[path] != "" && w.burpUsedFiles[path] != identity {
		return 0, errors.Join(errors.New("Burp source identity was revoked or source file reused"), w.clearBurp())
	}
	if w.burpIdentity != "" && w.burpIdentity != identity {
		previous := w.burpSource
		if err := w.clearBurp(); err != nil {
			return 0, err
		}
		if previous == path {
			return 0, errors.New("new Burp generation requires a new source file")
		}
	}
	if w.burpIdentity == "" {
		if err := w.removeBurpBoard(); err != nil {
			return 0, err
		}
	}
	w.burpIdentity, w.burpSource = identity, path
	if w.burpUsedFiles == nil {
		w.burpUsedFiles = map[string]string{}
	}
	w.burpUsedFiles[path] = identity
	f, info, err := openPrivateRegular(path, maxBurpSourceBytes)
	if err != nil {
		return 0, errors.Join(err, w.clearBurp())
	}
	defer f.Close()
	if w.states == nil {
		w.states = map[string]fileState{}
	}
	if w.seenBurpIDs == nil {
		w.seenBurpIDs = map[string]struct{}{}
	}
	st := w.states[path]
	if st.Info != nil && (!os.SameFile(st.Info, info) || info.Size() < st.Offset) {
		return 0, errors.Join(errors.New("Burp projection was replaced or truncated"), w.clearBurp())
	}
	chunk := w.MaxPollBytes
	if chunk <= 0 || chunk > maxPollBytes {
		chunk = maxPollBytes
	}
	if st.Offset > info.Size() {
		st = fileState{}
	}
	size := info.Size() - st.Offset
	if size > int64(chunk) {
		size = int64(chunk)
	}
	remaining := make([]byte, size)
	n, readErr := f.ReadAt(remaining, st.Offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) || n != len(remaining) {
		return 0, errors.Join(errors.New("Burp projection changed during read"), readErr, w.clearBurp())
	}
	remaining = remaining[:n]
	end := bytes.LastIndexByte(remaining, '\n')
	if end < 0 && len(remaining) == chunk {
		return 0, errors.Join(errors.New("Burp event line exceeds read budget"), w.clearBurp())
	}
	count := 0
	if end >= 0 {
		for _, line := range bytes.Split(remaining[:end], []byte{'\n'}) {
			st.Line++
			event, evidence, err := parseBurpEvent(line, m)
			if err != nil {
				if HasSecret(string(line)) {
					w.Adapter.RejectedSecret++
				} else {
					w.Adapter.RejectedInvalid++
				}
				continue
			}
			if _, exists := w.seenBurpIDs[event.EventID]; exists {
				continue
			}
			w.seenBurpIDs[event.EventID] = struct{}{}
			w.add(evidence)
			count++
		}
		st.Offset += int64(end + 1)
	}
	st.Info = info
	w.states[path] = st
	if count == 0 {
		return 0, nil
	}
	var board Board
	if w.Recommend != nil {
		board, err = w.Recommend(w.Events)
	} else {
		board = w.Library.Recommend(w.Events, 0)
	}
	if err != nil {
		return count, err
	}
	latest, latestPath, err := w.currentBurpManifest()
	if err != nil || latestPath != path || fmt.Sprintf("%s/%d/%s/%s", latest.SessionID, latest.Generation, latest.ScopeRevision, latest.FileName) != identity {
		return 0, errors.Join(errors.New("Burp source changed before board write"), w.clearBurp())
	}
	checkFile, checkInfo, err := openPrivateRegular(path, maxBurpSourceBytes)
	if err != nil || !os.SameFile(info, checkInfo) || checkInfo.Size() < st.Offset {
		if checkFile != nil {
			checkFile.Close()
		}
		return 0, errors.Join(errors.New("Burp projection changed before board write"), err, w.clearBurp())
	}
	checkFile.Close()
	if err := writeBurpBoard(w.BoardPath, []byte("# Atlas — board de melhor caminho\n\n"+board.Board)); err != nil {
		return 0, errors.Join(err, w.clearBurp())
	}
	return count, nil
}

func writeBurpBoard(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("Burp board directory must be private and regular")
	}
	f, err := os.CreateTemp(dir, ".atlas-board-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
