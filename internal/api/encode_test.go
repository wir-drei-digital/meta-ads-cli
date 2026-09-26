package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeForm(t *testing.T) {
	obj := map[string]any{
		"s": "x <b>", "n": json.Number("3000"), "b": true, "z": nil, "a": []any{},
		"o": map[string]any{"geo_locations": map[string]any{"countries": []any{"CH"}}, "html": "<a&b>"},
	}
	form, err := EncodeForm(obj)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{"s": {"x <b>"}, "n": {"3000"}, "b": {"true"}, "z": {"null"}, "a": {"[]"},
		"o": {`{"geo_locations":{"countries":["CH"]},"html":"<a&b>"}`}}
	for k, v := range want {
		if form.Get(k) != v[0] {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v[0])
		}
	}
	if len(form) != len(want) {
		t.Errorf("fields %v", form)
	}
}

func TestEncodeMultipart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "motiv.png")
	if err := os.WriteFile(p, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, ct, err := EncodeMultipart(url.Values{"name": {"img"}}, []File{{Field: "filename", Path: p}})
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q %v", ct, err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if form.Value["name"][0] != "img" || len(form.File["filename"]) != 1 {
		t.Fatalf("form %+v", form)
	}
	fh := form.File["filename"][0]
	if fh.Filename != "motiv.png" || fh.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("file header %+v", fh.Header)
	}
	f, _ := fh.Open()
	data, _ := io.ReadAll(f)
	if string(data) != "PNGDATA" {
		t.Fatalf("file content %q", data)
	}
	if _, _, err := EncodeMultipart(nil, []File{{Field: "filename", Path: filepath.Join(t.TempDir(), "missing.png")}}); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestProof(t *testing.T) {
	// printf '%s' token | openssl dgst -sha256 -hmac secret
	if got := Proof("token", "secret"); got != "e941110e3d2bfe82621f0e3e1434730d7305d106c5f68c87165d0b27a4611a4a" {
		t.Fatalf("proof %s", got)
	}
}
