package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry là một response của assistant (đã rút gọn) trong transcript.
type Entry struct {
	T     int64 // unix giây
	ID    string
	Model string
	In    int64
	CW5   int64
	CW1   int64
	CR    int64
	Out   int64
	Fast  bool
	Side  bool
}

type FileState struct {
	Size    int64
	MTime   int64
	Offset  int64 // byte đã đọc (luôn ở sau một '\n')
	Entries []Entry
	Last    *Entry // response chính (không sidechain) gần nhất -> context hiện tại
}

// Index được cache bằng gob ở ~/.cache/ctxbar/index.gob, đọc tăng dần theo offset.
type Index struct {
	Version int
	Files   map[string]*FileState
}

const indexVersion = 1

// Giữ dữ liệu 8 ngày: đủ cho cửa sổ 7 ngày + block 5h vắt qua.
const retention = 8 * 24 * time.Hour

func indexPath() string { return filepath.Join(cacheDir(), "index.gob") }

func loadIndex() *Index {
	ix := &Index{Version: indexVersion, Files: map[string]*FileState{}}
	f, err := os.Open(indexPath())
	if err != nil {
		return ix
	}
	defer f.Close()
	var got Index
	if gob.NewDecoder(f).Decode(&got) == nil && got.Version == indexVersion && got.Files != nil {
		return &got
	}
	return ix
}

func (ix *Index) save() error {
	if err := os.MkdirAll(cacheDir(), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir(), "index-*.tmp")
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(tmp).Encode(ix); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), indexPath())
}

// refresh quét projects/ của mọi tài khoản, chỉ đọc phần mới của các file thay đổi.
func (ix *Index) refresh(accs []Account, now time.Time) (changed bool) {
	since := now.Add(-retention)
	seen := map[string]bool{}
	for _, a := range accs {
		root := filepath.Join(a.Dir, "projects")
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(since) {
				return nil
			}
			seen[p] = true
			fsx := ix.Files[p]
			if fsx != nil && fsx.Size == info.Size() && fsx.MTime == info.ModTime().UnixNano() {
				return nil
			}
			if fsx == nil || info.Size() < fsx.Offset { // file mới hoặc bị ghi đè
				fsx = &FileState{}
				ix.Files[p] = fsx
			}
			if readFrom(p, fsx) == nil {
				fsx.Size, fsx.MTime = info.Size(), info.ModTime().UnixNano()
			}
			changed = true
			return nil
		})
	}
	cut := since.Unix()
	for p, fsx := range ix.Files {
		if !seen[p] {
			delete(ix.Files, p)
			changed = true
			continue
		}
		if len(fsx.Entries) > 0 && fsx.Entries[0].T < cut {
			kept := fsx.Entries[:0]
			for _, e := range fsx.Entries {
				if e.T >= cut {
					kept = append(kept, e)
				}
			}
			fsx.Entries = kept
			changed = true
		}
	}
	return changed
}

type rawLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	RequestID   string `json:"requestId"`
	Message     *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64  `json:"input_tokens"`
			CacheCreation int64  `json:"cache_creation_input_tokens"`
			CacheRead     int64  `json:"cache_read_input_tokens"`
			Output        int64  `json:"output_tokens"`
			Speed         string `json:"speed"`
			CC            *struct {
				E5 int64 `json:"ephemeral_5m_input_tokens"`
				E1 int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	kwUsage     = []byte(`"usage"`)
	kwAssistant = []byte(`"assistant"`)
)

func readFrom(path string, st *FileState) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(st.Offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// Dòng cuối chưa có '\n' (đang được ghi): để lần sau đọc lại.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		st.Offset += int64(len(line))
		if !bytes.Contains(line, kwUsage) || !bytes.Contains(line, kwAssistant) {
			continue
		}
		if e, ok := parseLine(line); ok {
			st.Entries = append(st.Entries, e)
			if !e.Side {
				ec := e
				st.Last = &ec
			}
		}
	}
}

func parseLine(line []byte) (Entry, bool) {
	var l rawLine
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return Entry{}, false
	}
	m := l.Message
	if m.Model == "" || m.Model == "<synthetic>" {
		return Entry{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return Entry{}, false
	}
	u := m.Usage
	e := Entry{
		T: t.Unix(), ID: m.ID + ":" + l.RequestID, Model: m.Model,
		In: u.Input, CR: u.CacheRead, Out: u.Output,
		Fast: u.Speed == "fast", Side: l.IsSidechain,
	}
	if u.CC != nil && u.CC.E5+u.CC.E1 > 0 {
		e.CW5, e.CW1 = u.CC.E5, u.CC.E1
	} else {
		e.CW5 = u.CacheCreation
	}
	return e, true
}
