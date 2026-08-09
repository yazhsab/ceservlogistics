package storage

import (
	"fmt"
	"net/http"
	"time"
)

// Config is the subset of application configuration this package needs. It is
// duplicated rather than imported so the platform packages stay acyclic:
// config must not depend on storage, and storage must not depend on config.
type Config struct {
	Driver       string
	Bucket       string
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	Dir          string
}

// Open builds the configured store.
//
// The two drivers are deliberately the only options: an in-memory store would
// make tests faster but would also let a bug that never writes bytes pass, and
// POD evidence is the last thing to discover is missing.
func Open(cfg Config) (Store, error) {
	switch cfg.Driver {
	case "s3":
		return NewS3Store(S3Config{
			Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket,
			AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey,
			UsePathStyle: cfg.UsePathStyle,
			HTTPClient:   &http.Client{Timeout: 60 * time.Second},
		})
	case "filesystem", "":
		return NewFSStore(cfg.Dir, cfg.Bucket)
	default:
		return nil, fmt.Errorf("storage: unknown driver %q", cfg.Driver)
	}
}
