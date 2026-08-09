// Package storage keeps binary objects outside PostgreSQL.
//
// Constitution §31: POD photos, signatures, labels and exports live in
// S3-compatible object storage; the database keeps only metadata. This package
// provides the Store interface and two implementations — S3 for production,
// filesystem for development and tests — so a test does not need a MinIO
// container to exercise POD upload and retrieval.
//
// The S3 implementation speaks the protocol directly with AWS Signature V4
// rather than pulling in a full SDK. That is a deliberate trade: the surface we
// need is four verbs (PUT, GET, DELETE, presign), SigV4 is a well-specified
// algorithm, and the alternative adds a large dependency tree to a codebase
// that is otherwise standard library plus five focused packages. See ADR 0010.
package storage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Errors the callers distinguish.
var (
	// ErrNotFound is returned when an object key does not exist.
	ErrNotFound = errors.New("storage: object not found")
	// ErrTooLarge is returned when an upload exceeds the configured limit.
	ErrTooLarge = errors.New("storage: object too large")
)

// Object describes a stored binary.
type Object struct {
	Key         string
	Bucket      string
	Size        int64
	ContentType string
	// Checksum is the SHA-256 of the stored bytes, computed while streaming.
	Checksum []byte
	ETag     string
}

// PutOptions tune one upload.
type PutOptions struct {
	ContentType string
	// MaxBytes aborts the upload once exceeded, so a client cannot fill the
	// disk by lying about Content-Length.
	MaxBytes int64
	Metadata map[string]string
}

// Store is the object-storage contract.
type Store interface {
	// Put streams an object in and returns its metadata.
	Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*Object, error)
	// Get streams an object out. The caller closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)
	// Delete removes an object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// SignedURL returns a time-limited retrieval URL, or "" when the backend
	// cannot presign (the filesystem store), in which case the caller streams
	// the bytes through the API instead.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Bucket names the container objects land in, for the metadata record.
	Bucket() string
}

// GenerateKey builds a storage key that is unguessable and collision-free.
//
// The key is server-generated from a random suffix and never derived from the
// client's filename: §31 requires a "random/generated object key", which is
// both the path-traversal control and what stops one upload overwriting
// another. The prefix is descriptive so an operator browsing the bucket can
// tell what they are looking at.
func GenerateKey(orgPublicID, purpose, extension string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// A failure here means the process has no entropy, which is not a
		// condition to paper over with a predictable key.
		panic("storage: cannot generate object key: " + err.Error())
	}
	now := time.Now().UTC()
	name := hex.EncodeToString(raw[:])
	if extension != "" {
		name += "." + strings.TrimPrefix(strings.ToLower(extension), ".")
	}
	return fmt.Sprintf("%s/%s/%s/%s",
		sanitizeSegment(orgPublicID),
		sanitizeSegment(strings.ToLower(purpose)),
		now.Format("2006/01/02"),
		name)
}

// sanitizeSegment keeps a path segment to safe characters. Belt and braces: the
// inputs are already server-controlled, but a key is a path and paths deserve
// paranoia.
func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Filesystem store
// ---------------------------------------------------------------------------

// FSStore keeps objects on local disk. Intended for development and tests; a
// production deployment uses S3Store so objects survive the container.
type FSStore struct {
	root   string
	bucket string
}

// NewFSStore builds a filesystem-backed store rooted at dir.
func NewFSStore(dir, bucket string) (*FSStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create root: %w", err)
	}
	return &FSStore{root: dir, bucket: bucket}, nil
}

// Bucket returns the logical bucket name.
func (s *FSStore) Bucket() string { return s.bucket }

func (s *FSStore) pathFor(key string) (string, error) {
	// Reject traversal outright rather than relying on path.Clean to clamp it.
	// Clean would silently rewrite "../../x" to "/x", which is safe but hides
	// the fact that something upstream produced a key it should not have.
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("storage: invalid object key")
	}
	clean := path.Clean("/" + key)
	full := filepath.Join(s.root, filepath.FromSlash(clean))
	// Belt and braces: confirm the result stays under the root. A path-traversal
	// bug here would let one tenant read another's delivery evidence.
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage: key escapes the root")
	}
	return full, nil
}

// Put writes an object to disk, hashing as it streams.
func (s *FSStore) Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*Object, error) {
	full, err := s.pathFor(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return nil, fmt.Errorf("storage: create directory: %w", err)
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("storage: create object: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	limited := limitReader(r, opts.MaxBytes)
	written, err := io.Copy(io.MultiWriter(f, hasher), limited)
	if err != nil {
		_ = os.Remove(full)
		return nil, err
	}
	return &Object{
		Key: key, Bucket: s.bucket, Size: written,
		ContentType: opts.ContentType, Checksum: hasher.Sum(nil),
	}, nil
}

// Get opens an object for reading.
func (s *FSStore) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	full, err := s.pathFor(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, &Object{Key: key, Bucket: s.bucket, Size: info.Size()}, nil
}

// Delete removes an object.
func (s *FSStore) Delete(ctx context.Context, key string) error {
	full, err := s.pathFor(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SignedURL returns "" — a filesystem store cannot presign, so the API streams
// the bytes after checking authorization.
func (s *FSStore) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "", nil
}

// ---------------------------------------------------------------------------
// S3-compatible store
// ---------------------------------------------------------------------------

// S3Config configures the S3-compatible backend.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// UsePathStyle addresses the bucket as /bucket/key rather than as a
	// subdomain. MinIO and most self-hosted gateways need it.
	UsePathStyle bool
	HTTPClient   *http.Client
}

// S3Store talks to an S3-compatible endpoint.
type S3Store struct {
	cfg    S3Config
	client *http.Client
	host   string
	scheme string
}

// NewS3Store builds an S3-backed store.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage: endpoint, bucket, access key and secret key are required")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: invalid endpoint: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &S3Store{cfg: cfg, client: client, host: u.Host, scheme: u.Scheme}, nil
}

// Bucket returns the bucket name.
func (s *S3Store) Bucket() string { return s.cfg.Bucket }

func (s *S3Store) objectURL(key string) string {
	escaped := escapePath(key)
	if s.cfg.UsePathStyle {
		return fmt.Sprintf("%s://%s/%s/%s", s.scheme, s.host, s.cfg.Bucket, escaped)
	}
	return fmt.Sprintf("%s://%s.%s/%s", s.scheme, s.cfg.Bucket, s.host, escaped)
}

// Put uploads an object.
//
// The body is buffered so the payload hash can be computed before signing: S3
// requires the content hash in the signature, and streaming variants trade that
// for chunked-upload complexity this codebase does not need. POD artifacts are
// bounded by MaxBytes well below any memory concern.
func (s *S3Store) Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*Object, error) {
	body, err := io.ReadAll(limitReader(r, opts.MaxBytes))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(body))
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}
	for k, v := range opts.Metadata {
		req.Header.Set("x-amz-meta-"+strings.ToLower(k), v)
	}
	// Private by default (§31): an object is readable only through a signed URL.
	req.Header.Set("x-amz-acl", "private")

	if err := s.sign(req, payloadHash, time.Now().UTC()); err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage: put object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, s3Error("put", resp)
	}
	return &Object{
		Key: key, Bucket: s.cfg.Bucket, Size: int64(len(body)),
		ContentType: opts.ContentType, Checksum: sum[:],
		ETag: strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

// Get downloads an object.
func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, nil, err
	}
	if err := s.sign(req, emptyPayloadHash, time.Now().UTC()); err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, nil, s3Error("get", resp)
	}
	return resp.Body, &Object{
		Key: key, Bucket: s.cfg.Bucket, Size: resp.ContentLength,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

// Delete removes an object.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	if err := s.sign(req, emptyPayloadHash, time.Now().UTC()); err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("storage: delete object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return s3Error("delete", resp)
	}
	return nil
}

// SignedURL issues a time-limited GET URL.
func (s *S3Store) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > 7*24*time.Hour {
		return "", errors.New("storage: signed URL lifetime must be between 1 second and 7 days")
	}
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405Z")
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", now.Format("20060102"), s.cfg.Region)

	raw := s.objectURL(key)
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.cfg.AccessKey+"/"+scope)
	q.Set("X-Amz-Date", stamp)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	u.RawQuery = q.Encode()

	canonical := strings.Join([]string{
		http.MethodGet,
		u.EscapedPath(),
		u.RawQuery,
		"host:" + u.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256.Sum256([]byte(canonical))
	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", stamp, scope, hex.EncodeToString(hashed[:]),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(s.signingKey(now), toSign))
	q.Set("X-Amz-Signature", signature)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// sign applies AWS Signature Version 4 to a request.
func (s *S3Store) sign(req *http.Request, payloadHash string, now time.Time) error {
	stamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", date, s.cfg.Region)

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("x-amz-date", stamp)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// Canonical headers: lowercase names, sorted, trimmed values.
	var names []string
	values := map[string]string{}
	for name, vs := range req.Header {
		lower := strings.ToLower(name)
		if lower != "host" && !strings.HasPrefix(lower, "x-amz-") && lower != "content-type" {
			continue
		}
		names = append(names, lower)
		values[lower] = strings.TrimSpace(strings.Join(vs, ","))
	}
	if _, ok := values["host"]; !ok {
		names = append(names, "host")
		values["host"] = req.URL.Host
	}
	sort.Strings(names)

	var canonicalHeaders strings.Builder
	for _, n := range names {
		canonicalHeaders.WriteString(n)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(values[n])
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	hashed := sha256.Sum256([]byte(canonical))
	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", stamp, scope, hex.EncodeToString(hashed[:]),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(s.signingKey(now), toSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKey, scope, signedHeaders, signature))
	return nil
}

func (s *S3Store) signingKey(now time.Time) []byte {
	kDate := hmacSHA256([]byte("AWS4"+s.cfg.SecretKey), now.Format("20060102"))
	kRegion := hmacSHA256(kDate, s.cfg.Region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// escapePath percent-encodes each key segment, leaving the separators.
func escapePath(key string) string {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func s3Error(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	// The response body can echo bucket names and request ids, so it is wrapped
	// rather than surfaced: an API client sees only a generic failure.
	return fmt.Errorf("storage: %s failed with status %d: %s", op, resp.StatusCode, strings.TrimSpace(string(body)))
}

// limitReader stops after max bytes and reports ErrTooLarge, rather than
// silently truncating.
func limitReader(r io.Reader, max int64) io.Reader {
	if max <= 0 {
		return r
	}
	return &boundedReader{r: io.LimitReader(r, max+1), max: max}
}

type boundedReader struct {
	r    io.Reader
	max  int64
	read int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += int64(n)
	if b.read > b.max {
		return n, ErrTooLarge
	}
	return n, err
}
