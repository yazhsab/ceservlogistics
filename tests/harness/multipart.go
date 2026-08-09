package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
)

// PostMultipart sends one file plus form fields, with the file's declared
// Content-Type under the caller's control.
//
// That control is the point: an attacker chooses the declared type and the
// filename, and the server must classify by content regardless. A helper that
// let multipart.Writer infer the type from the extension would test the
// library rather than the endpoint.
func (e *Env) PostMultipart(
	t *testing.T, path, token string,
	fields map[string]string,
	fileField, filename, declaredType string, content []byte,
	headers ...[2]string,
) Response {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if fileField != "" {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, filename))
		h.Set("Content-Type", declaredType)
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.URL(path), &buf)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, hdr := range headers {
		req.Header.Set(hdr[0], hdr[1])
	}

	resp, err := e.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := Response{Status: resp.StatusCode, Raw: string(raw), Headers: resp.Header}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}
