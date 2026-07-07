package gcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

type restClient struct {
	service *storage.Service
	bucket  string
}

func newRESTClient(service *storage.Service, bucket string) client {
	return &restClient{service: service, bucket: bucket}
}

// NewClient returns a GCS client backed by the Google Storage REST API.
func NewClient(service *storage.Service, bucket string) client {
	return newRESTClient(service, bucket)
}

func (c *restClient) getObject(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.service.Objects.Get(c.bucket, key).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("gcs client: get %s: %w", key, mapRESTError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcs client: read %s: %w", key, err)
	}
	return body, nil
}

func (c *restClient) putObject(
	ctx context.Context,
	key string,
	body []byte,
	opts putOptions,
) (objectInfo, error) {
	call := c.service.Objects.Insert(c.bucket, &storage.Object{Name: key}).
		Name(key).
		Media(bytes.NewReader(body))
	if opts.createOnly {
		call = call.IfGenerationMatch(0)
	}
	if opts.kmsKeyName != "" {
		call = call.KmsKeyName(opts.kmsKeyName)
	}
	obj, err := call.Context(ctx).Do()
	if err != nil {
		return objectInfo{}, fmt.Errorf("gcs client: put %s: %w", key, mapRESTError(err))
	}
	return objectInfo{name: obj.Name, generation: obj.Generation}, nil
}

func (c *restClient) deleteObject(ctx context.Context, key string, opts deleteOptions) error {
	call := c.service.Objects.Delete(c.bucket, key)
	if opts.generation != 0 {
		call = call.IfGenerationMatch(opts.generation)
	}
	if err := call.Context(ctx).Do(); err != nil {
		return fmt.Errorf("gcs client: delete %s: %w", key, mapRESTError(err))
	}
	return nil
}

func (c *restClient) listObjects(ctx context.Context, prefix string) ([]objectInfo, error) {
	var out []objectInfo
	call := c.service.Objects.List(c.bucket).Prefix(prefix)
	err := call.Pages(ctx, func(page *storage.Objects) error {
		for _, obj := range page.Items {
			out = append(out, objectInfo{name: obj.Name, generation: obj.Generation})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gcs client: list %s: %w", prefix, mapRESTError(err))
	}
	return out, nil
}

func mapRESTError(err error) error {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", errNotFound, apiErr.Message)
	case http.StatusPreconditionFailed:
		return fmt.Errorf("%w: %s", errPrecondition, apiErr.Message)
	default:
		return err
	}
}
