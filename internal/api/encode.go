package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// EncodeForm turns a JSON object into Graph API form fields, the encoding
// Meta documents for every write: strings as they are, numbers as written,
// booleans and null as their JSON text, arrays and objects as compact JSON.
func EncodeForm(obj map[string]any) (url.Values, error) {
	form := url.Values{}
	for k, v := range obj {
		switch x := v.(type) {
		case string:
			form.Set(k, x)
		case json.Number:
			form.Set(k, x.String())
		case bool:
			form.Set(k, strconv.FormatBool(x))
		case nil:
			form.Set(k, "null")
		default:
			raw, err := compactJSON(x)
			if err != nil {
				return nil, fmt.Errorf("field %s: %v", k, err)
			}
			form.Set(k, string(raw))
		}
	}
	return form, nil
}

// compactJSON marshals v without HTML escaping, so text such as "<b>" in an
// ad goes out as written.
func compactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// File is one --file part: the form field name and the local path.
type File struct{ Field, Path string }

// maxMultipart caps an upload so a wrong path (a disk image, a log file)
// fails locally instead of after minutes on the wire.
const maxMultipart = 100 << 20

// crlf removes CR and LF from a name in a part header: either would end the
// Content-Disposition line and start a header of the sender's choosing.
var crlf = strings.NewReplacer("\r", "", "\n", "")

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// EncodeMultipart writes fields and files as multipart/form-data and returns
// the body and its content type with the boundary. Each file part carries
// the content type its extension implies (image/png for .png).
func EncodeMultipart(fields url.Values, files []File) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range fields[k] {
			if err := w.WriteField(crlf.Replace(k), v); err != nil {
				return nil, "", err
			}
		}
	}
	for _, f := range files {
		src, err := os.Open(f.Path)
		if err != nil {
			return nil, "", err
		}
		info, err := src.Stat()
		if err != nil {
			src.Close()
			return nil, "", err
		}
		// Checked before copying: refusing a 2 GB file should not first pull
		// it into memory.
		if int64(buf.Len())+info.Size() > maxMultipart {
			src.Close()
			return nil, "", fmt.Errorf("the upload exceeds the 100 MB limit")
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(crlf.Replace(f.Field)), quoteEscaper.Replace(crlf.Replace(filepath.Base(f.Path)))))
		ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Path)))
		if ct == "" {
			ct = "application/octet-stream"
		}
		h.Set("Content-Type", ct)
		part, err := w.CreatePart(h)
		if err != nil {
			src.Close()
			return nil, "", err
		}
		_, err = io.Copy(part, src)
		src.Close()
		if err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}
