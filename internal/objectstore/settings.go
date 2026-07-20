package objectstore

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderAWS    = "aws"
	ProviderAliyun = "aliyun"
	ProviderR2     = "r2"
	ProviderMinIO  = "minio"

	BucketLookupAuto = "auto"
	BucketLookupDNS  = "dns"
	BucketLookupPath = "path"
)

type Settings struct {
	Provider         string
	Endpoint         string
	PublicEndpoint   string
	Region           string
	Bucket           string
	AccessKey        string
	SecretKey        string
	SessionToken     string
	UseSSL           bool
	BucketLookup     string
	AutoCreateBucket bool
	PresignTTL       time.Duration
}

func (s Settings) Validate() error {
	provider := strings.ToLower(strings.TrimSpace(s.Provider))
	switch provider {
	case ProviderAWS, ProviderAliyun, ProviderR2, ProviderMinIO:
	default:
		return fmt.Errorf("s3 provider must be one of aws, aliyun, r2, or minio")
	}
	if strings.TrimSpace(s.Endpoint) == "" {
		return fmt.Errorf("s3 endpoint is required")
	}
	if strings.TrimSpace(s.Bucket) == "" {
		return fmt.Errorf("s3 bucket is required")
	}
	if strings.TrimSpace(s.AccessKey) == "" || strings.TrimSpace(s.SecretKey) == "" {
		return fmt.Errorf("s3 access key and secret key are required")
	}
	switch strings.ToLower(strings.TrimSpace(s.BucketLookup)) {
	case "", BucketLookupAuto, BucketLookupDNS, BucketLookupPath:
	default:
		return fmt.Errorf("s3 bucket lookup must be auto, dns, or path")
	}
	return nil
}
