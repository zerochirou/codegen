package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func buildTestTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, body := range files {
		data := []byte(body)
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchRemoteFSAndMemFSCompliance(t *testing.T) {
	archive := buildTestTarGz(t, map[string]string{
		"codegen-master/README.md":                         "ignore me",
		"codegen-master/../../evil.txt":                    "traversal",
		"codegen-master/.agents/skills/new-skill/SKILL.md": "# new-skill\n",
		"codegen-master/.agents/skills/antislop/SKILL.md":  "# antislop\n",
		"codegen-master/.agents/skills/antislop/helper.py": "print('ok')\n",
		"codegen-master/.agents/rules/antislop.md":         "---\ntrigger: always_on\n---\nrule\n",
	})
	empty := buildTestTarGz(t, map[string]string{"codegen-empty/README.md": "none"})

	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			status := http.StatusNotFound
			var payload []byte
			switch r.URL.Path {
			case "/zerochirou/codegen/archive/master.tar.gz":
				status = http.StatusOK
				payload = archive
			case "/zerochirou/codegen/archive/empty.tar.gz":
				status = http.StatusOK
				payload = empty
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(bytes.NewReader(payload)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	remoteFS, err := FetchRemoteFS(context.Background(), RemoteOptions{
		Ref:        "master",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("FetchRemoteFS failed: %v", err)
	}

	if err := fstest.TestFS(remoteFS,
		".agents/skills/new-skill/SKILL.md",
		".agents/skills/antislop/SKILL.md",
		".agents/skills/antislop/helper.py",
		".agents/rules/antislop.md",
	); err != nil {
		t.Fatalf("memFS failed fstest.TestFS: %v", err)
	}

	if _, err := fs.Stat(remoteFS, "README.md"); err == nil {
		t.Fatalf("non-.agents files should not be kept in remote memFS")
	}

	if _, err := FetchRemoteFS(context.Background(), RemoteOptions{
		Ref:        "missing",
		HTTPClient: client,
	}); err == nil {
		t.Fatalf("expected error on HTTP 404")
	}

	if _, err := FetchRemoteFS(context.Background(), RemoteOptions{
		Ref:        "empty",
		HTTPClient: client,
	}); err == nil {
		t.Fatalf("expected error when archive contains no skills")
	}
}
