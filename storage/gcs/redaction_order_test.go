package gcs_test

import (
	"strings"
	"testing"

	"github.com/angvp/tango/storage/gcs"
)

func TestARedactedValueContainedInALongerOneLeavesNothingOfTheLongerOne(t *testing.T) {
	raw := []byte(`{"short":"abcd","long":"abcdLONGSECRET"}`)
	err := gcs.RedactCredentials("failed with abcdLONGSECRET", raw)
	if strings.Contains(err.Error(), "LONGSECRET") {
		t.Fatalf("error leaks part of a longer credential value: %q", err)
	}
}
