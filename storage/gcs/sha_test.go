package gcs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"

	"github.com/angvp/tango/storage"
)

const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

// Stat reports an object as absent unless its SHA-256 metadata is a lowercase
// digest, so a foreign object carrying another spelling is never trusted.
func TestStatTreatsANonLowercaseSHAMetadataAsAbsent(t *testing.T) {
	server := newFake(t)
	s := newStore(t, server.URL(), "")
	foreign := storage.NewKey()
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: foreign, ContentType: "text/plain", Metadata: map[string]string{"sha256": strings.ToUpper(helloSHA256)}},
		Content:     []byte("hello"),
	})
	if _, err := s.Stat(context.Background(), foreign); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat of an uppercase-digest object = %v, want ErrNotFound", err)
	}
	own := storage.NewKey()
	server.CreateObject(fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: own, ContentType: "text/plain", Metadata: map[string]string{"sha256": helloSHA256}},
		Content:     []byte("hello"),
	})
	info, err := s.Stat(context.Background(), own)
	if err != nil || info.SHA256 != helloSHA256 {
		t.Fatalf("Stat of a lowercase-digest object = %+v, %v; want SHA256 %s", info, err, helloSHA256)
	}
}
