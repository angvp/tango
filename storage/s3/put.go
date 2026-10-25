package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

// Put implements storage.Store. The upload is create-only and atomic: S3
// shows an object only once its upload completes.
func (s *Store) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (obj storage.Object, err error) {
	if err := ctx.Err(); err != nil {
		return storage.Object{}, err
	}
	inspected, err := storage.Inspect(r, opts)
	if err != nil {
		return storage.Object{}, err
	}
	spool, err := storagekit.NewSpoolFile("storage/s3", s.tempDir, "tango-s3-*", s.spoolOps)
	if err != nil {
		return storage.Object{}, err
	}
	// Release is best effort and its error is not returned: once the upload
	// has committed, a cleanup failure must not turn it into a failed Put.
	defer func() { _ = s.releaseSpool(spool) }()
	if err := spool.Fill(ctx, inspected); err != nil {
		return storage.Object{}, err
	}
	file, err := spool.Open()
	if err != nil {
		return storage.Object{}, err
	}
	obj = inspected.Object("")

	for attempt := 0; attempt < storagekit.KeyAttempts; attempt++ {
		key := s.nextKey()
		if !storage.ValidKey(key) {
			return storage.Object{}, fmt.Errorf("storage/s3: generated key %q is invalid", key)
		}
		err := s.upload(ctx, file, obj, key)
		if errors.Is(err, storage.ErrExists) {
			continue // a collision: the existing object is untouched
		}
		if err != nil {
			return storage.Object{}, tooLarge(err, opts)
		}
		obj.Key = key
		return obj, nil
	}
	return storage.Object{}, storage.ErrExists
}

// tooLarge reports a provider's own size refusal as storage.ErrTooLarge.
func tooLarge(err error, opts storage.PutOptions) error {
	var api smithy.APIError
	if errors.As(err, &api) && api.ErrorCode() == "EntityTooLarge" {
		return &storage.TooLargeError{Max: opts.MaxSize}
	}
	return err
}

// upload sends the spooled bytes as key: one PutObject when they fit in a
// part, otherwise a multipart upload read straight from the file.
func (s *Store) upload(ctx context.Context, file *os.File, obj storage.Object, key string) error {
	name := aws.String(s.objectName(key))
	metadata := map[string]string{shaMetadata: obj.SHA256}
	if obj.Size > s.partSize {
		return s.uploadParts(ctx, file, obj, name, metadata)
	}
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: &s.bucket, Key: name,
		Body:          io.NewSectionReader(file, 0, obj.Size),
		ContentLength: aws.Int64(obj.Size),
		ContentType:   aws.String(obj.ContentType),
		Metadata:      metadata,
		IfNoneMatch:   aws.String("*"),
	})
	if err != nil {
		return mapError(err)
	}
	return nil
}

// uploadParts is a sequential multipart upload. Only one part's worth of
// the file is in flight, and none of it is held in memory.
func (s *Store) uploadParts(ctx context.Context, file *os.File, obj storage.Object, name *string, metadata map[string]string) (err error) {
	created, err := s.client.CreateMultipartUpload(ctx, &awss3.CreateMultipartUploadInput{
		Bucket: &s.bucket, Key: name, ContentType: aws.String(obj.ContentType), Metadata: metadata,
	})
	if err != nil {
		return mapError(err)
	}
	defer func() {
		if err == nil {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
		defer cancel()
		_, _ = s.client.AbortMultipartUpload(abortCtx, &awss3.AbortMultipartUploadInput{Bucket: &s.bucket, Key: name, UploadId: created.UploadId})
	}()

	partSize := s.partSize
	if need := (obj.Size + maxParts - 1) / maxParts; need > partSize {
		partSize = need
	}
	var parts []types.CompletedPart
	for offset, number := int64(0), int32(1); offset < obj.Size; offset, number = offset+partSize, number+1 {
		size := min(partSize, obj.Size-offset)
		uploaded, err := s.client.UploadPart(ctx, &awss3.UploadPartInput{
			Bucket: &s.bucket, Key: name, UploadId: created.UploadId, PartNumber: aws.Int32(number),
			Body: io.NewSectionReader(file, offset, size), ContentLength: aws.Int64(size),
		})
		if err != nil {
			return mapError(err)
		}
		parts = append(parts, types.CompletedPart{ETag: uploaded.ETag, PartNumber: aws.Int32(number)})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket: &s.bucket, Key: name, UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
		IfNoneMatch:     aws.String("*"),
	})
	if err != nil {
		return mapError(err)
	}
	return nil
}

// releaseSpoolFile is the default release: best effort, see storagekit.SpoolFile.
func releaseSpoolFile(sp *storagekit.SpoolFile) error { return sp.Release() }
