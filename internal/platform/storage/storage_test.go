package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGenerateKeyIsUnguessableAndScoped(t *testing.T) {
	a := GenerateKey("org_01ABC", "POD_PHOTO", "jpg")
	b := GenerateKey("org_01ABC", "POD_PHOTO", "jpg")
	if a == b {
		t.Fatal("two generated keys collided")
	}
	if !strings.HasPrefix(a, "org_01ABC/pod_photo/") {
		t.Errorf("key should be scoped by organization and purpose, got %q", a)
	}
	if !strings.HasSuffix(a, ".jpg") {
		t.Errorf("key should carry the extension, got %q", a)
	}
	// 128 bits of randomness in the filename is what makes a key unguessable.
	name := a[strings.LastIndex(a, "/")+1:]
	if len(strings.TrimSuffix(name, ".jpg")) != 32 {
		t.Errorf("expected a 32-character hex name, got %q", name)
	}
}

func TestGenerateKeyRejectsPathInjection(t *testing.T) {
	// A hostile organization id or purpose must not be able to introduce path
	// separators or traversal segments.
	key := GenerateKey("../../etc", "../passwd", "png")
	if strings.Contains(key[:strings.LastIndex(key, "/")], "..") {
		t.Errorf("key contains a traversal segment: %q", key)
	}
	if strings.Count(key, "/") != 5 {
		t.Errorf("unexpected key shape: %q", key)
	}
}

func TestFSStoreRoundTrip(t *testing.T) {
	store, err := NewFSStore(t.TempDir(), "test-bucket")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	payload := []byte("signature-bytes")
	want := sha256.Sum256(payload)

	obj, err := store.Put(ctx, "a/b/c.png", bytes.NewReader(payload), PutOptions{
		ContentType: "image/png", MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if obj.Size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", obj.Size, len(payload))
	}
	if hex.EncodeToString(obj.Checksum) != hex.EncodeToString(want[:]) {
		t.Error("checksum does not match the uploaded bytes")
	}
	if obj.Bucket != "test-bucket" {
		t.Errorf("bucket = %q", obj.Bucket)
	}

	rc, got, err := store.Get(ctx, "a/b/c.png")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	read, _ := io.ReadAll(rc)
	if !bytes.Equal(read, payload) {
		t.Error("retrieved bytes differ from what was stored")
	}
	if got.Size != int64(len(payload)) {
		t.Errorf("size on read = %d", got.Size)
	}

	if err := store.Delete(ctx, "a/b/c.png"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, "a/b/c.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	// Deleting twice is not an error: retention sweeps must be idempotent.
	if err := store.Delete(ctx, "a/b/c.png"); err != nil {
		t.Errorf("second delete should succeed, got %v", err)
	}
}

func TestFSStoreRefusesPathEscape(t *testing.T) {
	store, err := NewFSStore(t.TempDir(), "b")
	if err != nil {
		t.Fatal(err)
	}
	// Even though keys are generated, a defect elsewhere must not be able to
	// write outside the root.
	_, err = store.Put(context.Background(), "../../escape.txt",
		strings.NewReader("x"), PutOptions{MaxBytes: 100})
	if err == nil {
		t.Fatal("expected a path escape to be refused")
	}
}

func TestFSStoreEnforcesSizeLimit(t *testing.T) {
	store, err := NewFSStore(t.TempDir(), "b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(context.Background(), "big.bin",
		bytes.NewReader(make([]byte, 5000)), PutOptions{MaxBytes: 1024})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	// The partial file must not survive a rejected upload.
	if _, _, gErr := store.Get(context.Background(), "big.bin"); !errors.Is(gErr, ErrNotFound) {
		t.Errorf("a rejected upload left a file behind: %v", gErr)
	}
}

func TestFSStoreDoesNotPresign(t *testing.T) {
	store, _ := NewFSStore(t.TempDir(), "b")
	url, err := store.SignedURL(context.Background(), "k", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if url != "" {
		t.Errorf("filesystem store should not presign, got %q", url)
	}
}

func TestS3StorePutSignsRequest(t *testing.T) {
	var (
		gotAuth   string
		gotSHA    string
		gotACL    string
		gotBody   []byte
		gotMethod string
		gotPath   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSHA = r.Header.Get("x-amz-content-sha256")
		gotACL = r.Header.Get("x-amz-acl")
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store, err := NewS3Store(S3Config{
		Endpoint: srv.URL, Bucket: "pod", Region: "ap-south-1",
		AccessKey: "AKIAEXAMPLE", SecretKey: "secret", UsePathStyle: true,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("photo")
	obj, err := store.Put(context.Background(), "org/pod/x.jpg", bytes.NewReader(payload),
		PutOptions{ContentType: "image/jpeg", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s", gotMethod)
	}
	if gotPath != "/pod/org/pod/x.jpg" {
		t.Errorf("path = %q, want path-style /pod/org/pod/x.jpg", gotPath)
	}
	if !bytes.Equal(gotBody, payload) {
		t.Error("body did not arrive intact")
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/") {
		t.Errorf("authorization header not SigV4: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "SignedHeaders=") || !strings.Contains(gotAuth, "Signature=") {
		t.Errorf("authorization header incomplete: %q", gotAuth)
	}
	want := sha256.Sum256(payload)
	if gotSHA != hex.EncodeToString(want[:]) {
		t.Errorf("content hash = %q", gotSHA)
	}
	// §31: storage is private; an object must never be world-readable.
	if gotACL != "private" {
		t.Errorf("acl = %q, want private", gotACL)
	}
	if obj.ETag != "abc123" {
		t.Errorf("etag = %q", obj.ETag)
	}
	if hex.EncodeToString(obj.Checksum) != hex.EncodeToString(want[:]) {
		t.Error("returned checksum does not match the uploaded bytes")
	}
}

func TestS3StoreGetMapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	store, _ := NewS3Store(S3Config{
		Endpoint: srv.URL, Bucket: "b", AccessKey: "a", SecretKey: "s",
		UsePathStyle: true, HTTPClient: srv.Client(),
	})
	if _, _, err := store.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestS3SignedURLShape(t *testing.T) {
	store, _ := NewS3Store(S3Config{
		Endpoint: "https://s3.example.com", Bucket: "pod", Region: "ap-south-1",
		AccessKey: "AKIAEXAMPLE", SecretKey: "secret", UsePathStyle: true,
	})
	url, err := store.SignedURL(context.Background(), "a/b/c.jpg", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"X-Amz-Algorithm=AWS4-HMAC-SHA256",
		"X-Amz-Credential=AKIAEXAMPLE",
		"X-Amz-Expires=900",
		"X-Amz-SignedHeaders=host",
		"X-Amz-Signature=",
	} {
		if !strings.Contains(url, want) {
			t.Errorf("signed URL missing %s: %s", want, url)
		}
	}
	// The secret must never appear in the URL.
	if strings.Contains(url, "secret") {
		t.Error("signed URL leaks the secret key")
	}
}

func TestS3SignedURLRejectsUnreasonableTTL(t *testing.T) {
	store, _ := NewS3Store(S3Config{
		Endpoint: "https://s3.example.com", Bucket: "b",
		AccessKey: "a", SecretKey: "s",
	})
	for _, ttl := range []time.Duration{0, -time.Minute, 8 * 24 * time.Hour} {
		if _, err := store.SignedURL(context.Background(), "k", ttl); err == nil {
			t.Errorf("ttl %s should be refused", ttl)
		}
	}
}

func TestS3StoreRequiresCredentials(t *testing.T) {
	if _, err := NewS3Store(S3Config{Endpoint: "https://x", Bucket: "b"}); err == nil {
		t.Fatal("expected missing credentials to be refused")
	}
}
