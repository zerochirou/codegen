package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultRepo is the upstream repository used by `codegen skills update --remote`.
	DefaultRepo = "zerochirou/codegen"
	// DefaultRef is the default git ref fetched by `--remote`.
	DefaultRef = "master"

	maxRemoteArchiveBytes = 32 << 20
	maxRemoteFileBytes    = 4 << 20
)

// RemoteOptions configures fetching bundled skills and rules from GitHub.
type RemoteOptions struct {
	Repo       string
	Ref        string
	BaseURL    string
	HTTPClient *http.Client
}

// FetchRemoteFS downloads the GitHub archive tarball for Repo/Ref and extracts
// `.agents/skills/...` and `.agents/rules/...` into an in-memory fs.FS.
func FetchRemoteFS(ctx context.Context, opts RemoteOptions) (fs.FS, error) {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(os.Getenv("CODEGEN_SKILLS_REPO"))
	}
	if repo == "" {
		repo = DefaultRepo
	}

	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		ref = DefaultRef
	}

	archiveURL := buildArchiveURL(opts.BaseURL, repo, ref)
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create remote skills request: %w", err)
	}
	req.Header.Set("User-Agent", "codegen-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch remote skills from %s: %w", archiveURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch remote skills from %s: HTTP %d", archiveURL, resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxRemoteArchiveBytes+1)
	gz, err := gzip.NewReader(limited)
	if err != nil {
		return nil, fmt.Errorf("decompress remote skills archive: %w", err)
	}
	defer gz.Close()

	files := make(map[string][]byte)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read remote skills archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}

		rel, ok := normalizeArchiveAssetPath(hdr.Name)
		if !ok {
			continue
		}
		if hdr.Size > maxRemoteFileBytes {
			return nil, fmt.Errorf("remote asset %s exceeds maximum allowed size (%d bytes)", rel, maxRemoteFileBytes)
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxRemoteFileBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read remote asset %s: %w", rel, err)
		}
		if len(data) > maxRemoteFileBytes {
			return nil, fmt.Errorf("remote asset %s exceeds maximum allowed size (%d bytes)", rel, maxRemoteFileBytes)
		}
		files[rel] = data
	}

	mfs := newMemFS(files)
	skills, err := discoverBundledSkills(mfs)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("remote archive %s (%s) contained no skills under .agents/skills", repo, ref)
	}
	return mfs, nil
}

func buildArchiveURL(baseURL, repo, ref string) string {
	escapedRef := url.PathEscape(ref)
	if strings.TrimSpace(baseURL) != "" {
		base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		return fmt.Sprintf("%s/%s.tar.gz", base, escapedRef)
	}
	return fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", repo, escapedRef)
}

func normalizeArchiveAssetPath(raw string) (string, bool) {
	cleaned := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "/") {
		return "", false
	}

	candidate := cleaned
	if !strings.HasPrefix(candidate, ".agents/skills/") && !strings.HasPrefix(candidate, ".agents/rules/") {
		// GitHub tarballs wrap files inside a single top-level directory (`<repo>-<ref>/...`).
		_, rest, hasRoot := strings.Cut(cleaned, "/")
		if !hasRoot || rest == "" {
			return "", false
		}
		candidate = rest
	}

	if !fs.ValidPath(candidate) {
		return "", false
	}
	if !strings.HasPrefix(candidate, ".agents/skills/") && !strings.HasPrefix(candidate, ".agents/rules/") {
		return "", false
	}
	return candidate, true
}

type memFS struct {
	files map[string][]byte
	dirs  map[string][]string
}

func newMemFS(files map[string][]byte) *memFS {
	children := map[string]map[string]struct{}{
		".": {},
	}
	ensureDir := func(dir string) {
		if _, ok := children[dir]; !ok {
			children[dir] = map[string]struct{}{}
		}
	}

	for p := range files {
		if !fs.ValidPath(p) {
			continue
		}
		cur := p
		for cur != "." {
			parent := path.Dir(cur)
			ensureDir(parent)
			children[parent][path.Base(cur)] = struct{}{}
			if cur != p {
				ensureDir(cur)
			}
			cur = parent
		}
	}

	dirs := make(map[string][]string, len(children))
	for dir, set := range children {
		names := make([]string, 0, len(set))
		for name := range set {
			names = append(names, name)
		}
		slices.Sort(names)
		dirs[dir] = names
	}

	return &memFS{
		files: files,
		dirs:  dirs,
	}
}

func (m *memFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := m.files[name]; ok {
		return &memFile{
			name:   path.Base(name),
			data:   data,
			reader: bytes.NewReader(data),
		}, nil
	}
	if entries, ok := m.dirs[name]; ok {
		return &memDir{
			fs:      m,
			path:    name,
			entries: entries,
		}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (m *memFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	data, ok := m.files[name]
	if !ok {
		if _, isDir := m.dirs[name]; isDir {
			return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
		}
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (m *memFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := m.files[name]; ok {
		return memFileInfo{name: path.Base(name), size: int64(len(data))}, nil
	}
	if _, ok := m.dirs[name]; ok {
		base := path.Base(name)
		if name == "." {
			base = "."
		}
		return memFileInfo{name: base, isDir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (m *memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if _, isFile := m.files[name]; isFile {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	names, ok := m.dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries := make([]fs.DirEntry, 0, len(names))
	for _, child := range names {
		full := child
		if name != "." {
			full = name + "/" + child
		}
		info, err := m.Stat(full)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	return entries, nil
}

type memFile struct {
	name   string
	data   []byte
	reader *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return memFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *memFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *memFile) Close() error {
	return nil
}

type memDir struct {
	fs      *memFS
	path    string
	entries []string
	offset  int
}

func (d *memDir) Stat() (fs.FileInfo, error) {
	return d.fs.Stat(d.path)
}

func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.path, Err: fs.ErrInvalid}
}

func (d *memDir) Close() error {
	return nil
}

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.offset >= len(d.entries) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	end := len(d.entries)
	if n > 0 && d.offset+n < end {
		end = d.offset + n
	}
	out := make([]fs.DirEntry, 0, end-d.offset)
	for _, child := range d.entries[d.offset:end] {
		full := child
		if d.path != "." {
			full = d.path + "/" + child
		}
		info, err := d.fs.Stat(full)
		if err != nil {
			return nil, err
		}
		out = append(out, fs.FileInfoToDirEntry(info))
	}
	d.offset = end
	return out, nil
}

type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i memFileInfo) Name() string { return i.name }
func (i memFileInfo) Size() int64  { return i.size }
func (i memFileInfo) Mode() fs.FileMode {
	if i.isDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return i.isDir }
func (i memFileInfo) Sys() any           { return nil }
