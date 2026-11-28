package journal

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestNewR2AcceptsOnlyAScopedEUJournal(t *testing.T) {
	credentials := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "fixture-access-key", SecretAccessKey: "fixture-secret-key"}, nil
	})
	index, envelope := [32]byte{1}, [32]byte{2}
	journal, err := NewR2(testAccount, testBucket, "portal", index, envelope, credentials)
	if err != nil || journal == nil {
		t.Fatalf("NewR2 refused a valid configuration: %v", err)
	}
	if journal.account != testAccount || journal.bucket != testBucket || journal.namespace != "portal" {
		t.Fatalf("NewR2 bound %q/%q/%q", journal.account, journal.bucket, journal.namespace)
	}
	for name, attempt := range map[string]func() (*Journal, error){
		"short account": func() (*Journal, error) { return NewR2("1234", testBucket, "portal", index, envelope, credentials) },
		"uppercase account": func() (*Journal, error) {
			return NewR2("1234567890ABCDEF1234567890ABCDEF", testBucket, "portal", index, envelope, credentials)
		},
		"invalid bucket": func() (*Journal, error) {
			return NewR2(testAccount, "Bucket_Name", "portal", index, envelope, credentials)
		},
		"path namespace": func() (*Journal, error) {
			return NewR2(testAccount, testBucket, "portal/other", index, envelope, credentials)
		},
		"missing credentials": func() (*Journal, error) { return NewR2(testAccount, testBucket, "portal", index, envelope, nil) },
		"missing index key": func() (*Journal, error) {
			return NewR2(testAccount, testBucket, "portal", [32]byte{}, envelope, credentials)
		},
		"missing envelope": func() (*Journal, error) {
			return NewR2(testAccount, testBucket, "portal", index, [32]byte{}, credentials)
		},
		"shared key": func() (*Journal, error) { return NewR2(testAccount, testBucket, "portal", index, index, credentials) },
	} {
		if journal, err := attempt(); !errors.Is(err, ErrUnconfirmed) || journal != nil {
			t.Errorf("%s: NewR2 returned %v, %v; want refusal", name, journal, err)
		}
	}
}
