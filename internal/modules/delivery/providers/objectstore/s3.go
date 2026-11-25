// Package providers contains bounded external SDK adapters. No adapter posts money.
package objectstore

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3 struct {
	client    *s3.Client
	presign   *s3.PresignClient
	bucket    string
	transport *http.Transport
}

func NewS3(endpoint, region, bucket, key, secret string, pathStyle bool) *S3 {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.MaxConnsPerHost = 4
	t.MaxIdleConnsPerHost = 4
	t.ResponseHeaderTimeout = 5 * time.Second
	client := s3.NewFromConfig(aws.Config{Region: region, Credentials: credentials.NewStaticCredentialsProvider(key, secret, ""), HTTPClient: &http.Client{Transport: t, Timeout: 10 * time.Second}, RetryMaxAttempts: 1, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired}, func(o *s3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = pathStyle })
	return &S3{client, s3.NewPresignClient(client), bucket, t}
}
func (s *S3) Close() error { s.transport.CloseIdleConnections(); return nil }
func (s *S3) Presign(ctx context.Context, key, mime string, size int64, sum string, ttl time.Duration) (string, http.Header, error) {
	if size < 1 || size > delivery.MaxImageBytes || ttl <= 0 || ttl > 5*time.Minute {
		return "", nil, errors.New("invalid upload constraints")
	}
	v, e := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(mime), ContentLength: aws.Int64(size), ChecksumSHA256: aws.String(sum), ACL: types.ObjectCannedACLPrivate}, func(o *s3.PresignOptions) { o.Expires = ttl })
	if e != nil {
		return "", nil, e
	}
	return v.URL, v.SignedHeader, nil
}
func (s *S3) Head(ctx context.Context, key string) (delivery.Object, error) {
	v, e := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if e != nil {
		return delivery.Object{}, e
	}
	return delivery.Object{Size: aws.ToInt64(v.ContentLength), ContentType: aws.ToString(v.ContentType), ETag: aws.ToString(v.ETag)}, nil
}
func (s *S3) Read(ctx context.Context, key, etag string, max int64) ([]byte, error) {
	if max < 1 || max > delivery.MaxImageBytes {
		return nil, errors.New("invalid read bound")
	}
	v, e := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), IfMatch: aws.String(etag)})
	if e != nil {
		return nil, e
	}
	defer v.Body.Close()
	b, e := io.ReadAll(io.LimitReader(v.Body, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > max {
		return nil, errors.New("object exceeds bound")
	}
	return b, nil
}
func (s *S3) Put(ctx context.Context, key string, b []byte, mime string) error {
	if len(b) > 8<<20 {
		return errors.New("object exceeds bound")
	}
	_, e := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(b), ContentType: aws.String(mime), CacheControl: aws.String("public,max-age=31536000,immutable"), ACL: types.ObjectCannedACLPrivate})
	return e
}
func (s *S3) Delete(ctx context.Context, key string) error {
	_, e := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return e
}
