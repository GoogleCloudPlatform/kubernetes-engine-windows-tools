// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package builder

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

// Create the GCS bucket if it doesn't exist. The bucket is used to copy workspace over to Windows instances.
func NewGCSBucketIfNotExists(ctx context.Context, projectID string, workspaceBucket string) error {
	if workspaceBucket == "" {
		log.Printf("No bucket name specified, skip creating the bucket")
		return nil
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("Storage client creation failed: %+v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	lifecycleAttrs := &storage.BucketAttrs{
		Lifecycle: storage.Lifecycle{
			Rules: []storage.LifecycleRule{
				{
					Action: storage.LifecycleAction{Type: "Delete"},
					Condition: storage.LifecycleCondition{
						AgeInDays: 1,
					},
				},
			},
		},
	}
	bkt := client.Bucket(workspaceBucket)
	if err := bkt.Create(ctx, projectID, lifecycleAttrs); err != nil {
		if e, ok := err.(*googleapi.Error); ok && e.Code == 409 {
			log.Printf("%v bucket already exists", workspaceBucket)
			return nil
		}
		return fmt.Errorf("Create bucket(%q) with error: %+v", workspaceBucket, err)
	}
	log.Printf("Bucket %v is setup", workspaceBucket)
	return nil
}

func writeZipToBucket(
	ctx context.Context,
	bucket string,
	object string,
	inputPath string,
) (string, error) {
	zp, err := createZip(ctx, inputPath)
	if err != nil {
		return "", err
	}

	return writeToBucket(ctx, bucket, object, zp)
}

func writeToBucket(
	ctx context.Context,
	bucket string,
	object string,
	inputPath string,
) (string, error) {

	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()

	bkt := client.Bucket(bucket)

	f, err := os.Open(inputPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	obj := bkt.Object(object)
	w := obj.NewWriter(ctx)
	defer w.Close()

	if _, err := io.Copy(w, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("gs://%s/%s", bucket, object), nil
}

func createZip(ctx context.Context, fullpath string) (string, error) {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer f.Close()

	zipW := zip.NewWriter(f)
	defer zipW.Close()

	err = filepath.Walk(fullpath, func(path string, info os.FileInfo, err error) error {
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if fi.IsDir() {
			// Skip
			return ctx.Err()
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			log.Printf("Skipping symlink: %q", path)
			return ctx.Err()
		}

		trimmedPath := path
		if filepath.HasPrefix(trimmedPath, fullpath) {
			trimmedPath = trimmedPath[len(fullpath)+1:]
		}

		w, err := zipW.Create(trimmedPath)
		if err != nil {
			return err
		}
		if err := copyFile(w, path); err != nil {
			return err
		}

		return ctx.Err()
	})

	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %v", err)
	}

	return f.Name(), ctx.Err()
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}
