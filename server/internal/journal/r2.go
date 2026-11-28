package journal

import (
	"net/http"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	r2Account = regexp.MustCompile(`^[0-9a-f]{32}$`)
	r2Bucket  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
	scope     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// NewR2 binds a private journal to one EU R2 bucket and a portal instance.
// The caller supplies separate scoped credentials and keys retained for the restore window.
func NewR2(account, bucket, namespace string, indexKey, envelopeKey [32]byte, credentials aws.CredentialsProvider) (*Journal, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, ErrUnconfirmed
	}
	transport := base.Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	return newR2(account, bucket, namespace, indexKey, envelopeKey, credentials, transport)
}

func newR2(account, bucket, namespace string, indexKey, envelopeKey [32]byte, credentials aws.CredentialsProvider, transport http.RoundTripper) (*Journal, error) {
	if !r2Account.MatchString(account) || !r2Bucket.MatchString(bucket) || !scope.MatchString(namespace) || credentials == nil {
		return nil, ErrUnconfirmed
	}
	client := s3.New(s3.Options{
		Region: "auto", BaseEndpoint: aws.String("https://" + account + ".eu.r2.cloudflarestorage.com"), UsePathStyle: true,
		Credentials: credentials, Retryer: aws.NopRetryer{},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		HTTPClient: &http.Client{
			Transport: transport, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	})
	return newJournal(client, account, bucket, namespace, indexKey, envelopeKey)
}
