package s3_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/storage"
)

const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

// Stat reports an object as absent unless its SHA-256 metadata is a lowercase
// digest, so a foreign object carrying another spelling is never trusted.
func TestStatTreatsANonLowercaseSHAMetadataAsAbsent(t *testing.T) {
	fake, server := newFakeS3(t, testBucket)
	s := newStore(t, server.URL, "", nil)
	foreign := storage.NewKey()
	fake.objects[foreign] = &fakeObject{data: []byte("hello"), headers: http.Header{"Content-Type": {"text/plain"}, "X-Amz-Meta-Sha256": {strings.ToUpper(helloSHA256)}}, modified: time.Now()}
	if _, err := s.Stat(context.Background(), foreign); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat of an uppercase-digest object = %v, want ErrNotFound", err)
	}
	own := storage.NewKey()
	fake.objects[own] = &fakeObject{data: []byte("hello"), headers: http.Header{"Content-Type": {"text/plain"}, "X-Amz-Meta-Sha256": {helloSHA256}}, modified: time.Now()}
	info, err := s.Stat(context.Background(), own)
	if err != nil || info.SHA256 != helloSHA256 {
		t.Fatalf("Stat of a lowercase-digest object = %+v, %v; want SHA256 %s", info, err, helloSHA256)
	}
}
