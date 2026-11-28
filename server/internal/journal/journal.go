// Package journal stores account removal evidence outside both restored databases.
package journal

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	awsHTTP "github.com/aws/smithy-go/transport/http"
	"github.com/devantler-tech/business-site/server/internal/identity"
)

// ErrUnconfirmed leaves the operator request or restored account on hold.
var ErrUnconfirmed = errors.New("account deletion evidence could not be verified")

// State describes the independently authenticated records for one account.
type State uint8

const (
	// Pending means replay must hold this account for resolution.
	Pending State = iota
	// Confirmed means replay may proceed through the native provider guard.
	Confirmed
)

type objectStore interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// Journal stores exact encrypted intents and confirmations under separate immutable keys.
type Journal struct {
	store                      objectStore
	account, bucket, namespace string
	indexKey                   [32]byte
	envelope                   cipher.AEAD
}

var _ identity.CustomerAccountDeletionJournal = (*Journal)(nil)

var nativeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

const (
	maxRecord = 4096
	intent    = "/1-intent"
	confirmed = "/2-confirmation"
)

type record struct {
	Issuer string
	Target identity.CustomerDeletionTarget
}

func newJournal(store objectStore, account, bucket, namespace string, indexKey, envelopeKey [32]byte) (*Journal, error) {
	if store == nil || indexKey == ([32]byte{}) || envelopeKey == ([32]byte{}) || indexKey == envelopeKey {
		return nil, ErrUnconfirmed
	}
	block, err := aes.NewCipher(envelopeKey[:])
	if err != nil {
		return nil, ErrUnconfirmed
	}
	envelope, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrUnconfirmed
	}
	return &Journal{store: store, account: account, bucket: bucket, namespace: namespace, indexKey: indexKey, envelope: envelope}, nil
}

// Register initializes the independent store during separate provisioning.
// The portal and the restore path require this record before using the journal.
func (j *Journal) Register(ctx context.Context) error {
	key, expected := j.registration()
	return j.write(ctx, key, expected)
}

func (j *Journal) verifyRegistration(ctx context.Context) error {
	key, expected := j.registration()
	found, err := j.matches(ctx, key, expected)
	if err != nil || !found {
		return ErrUnconfirmed
	}
	return nil
}

func (j *Journal) registration() (string, []byte) {
	key := j.namespacePrefix() + "registration"
	mac := hmac.New(sha256.New, j.indexKey[:])
	_, _ = io.WriteString(mac, j.account+"/"+j.bucket+"/"+key)
	return key, mac.Sum(nil)
}

// RecordDeletionIntent verifies a durable record before the provider mutation.
func (j *Journal) RecordDeletionIntent(ctx context.Context, issuer string, target identity.CustomerDeletionTarget) error {
	key, expected, err := j.binding(issuer, target)
	if err != nil {
		return err
	}
	if err := j.verifyRegistration(ctx); err != nil {
		return err
	}
	return j.write(ctx, key+intent, expected)
}

// RecordDeletionConfirmation requires the existing intent and writes its distinct confirmation.
func (j *Journal) RecordDeletionConfirmation(ctx context.Context, issuer string, target identity.CustomerDeletionTarget) error {
	key, expected, err := j.binding(issuer, target)
	if err != nil {
		return err
	}
	if err := j.verifyRegistration(ctx); err != nil {
		return err
	}
	found, err := j.matches(ctx, key+intent, expected)
	if err != nil || !found {
		return ErrUnconfirmed
	}
	return j.write(ctx, key+confirmed, expected)
}

func (j *Journal) binding(issuer string, target identity.CustomerDeletionTarget) (string, []byte, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		len(issuer) > 500 || !nativeID.MatchString(target.UserID) || !nativeID.MatchString(target.Organization) || target.CreatedAt.IsZero() {
		return "", nil, ErrUnconfirmed
	}
	target.CreatedAt = target.CreatedAt.UTC()
	expected, err := json.Marshal(record{Issuer: issuer, Target: target})
	if err != nil || len(expected)+j.envelope.Overhead() > maxRecord {
		return "", nil, ErrUnconfirmed
	}
	prefix := j.prefix()
	mac := hmac.New(sha256.New, j.indexKey[:])
	_, _ = io.WriteString(mac, prefix)
	_, _ = mac.Write(expected)
	return prefix + hex.EncodeToString(mac.Sum(nil)), expected, nil
}

func (j *Journal) prefix() string {
	return j.namespacePrefix() + "accounts/"
}

func (j *Journal) namespacePrefix() string {
	return "account-deletion/v1/" + j.namespace + "/"
}

func (j *Journal) write(ctx context.Context, key string, expected []byte) error {
	data := j.envelope.Seal(nil, nil, expected, []byte(key))
	_, err := j.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(j.bucket), Key: aws.String(key), Body: bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String("application/octet-stream"),
		IfNoneMatch: aws.String("*"),
	})
	if err != nil && responseStatus(err) != http.StatusPreconditionFailed && !bucketLocked(err) {
		return ErrUnconfirmed
	}
	found, err := j.matches(ctx, key, expected)
	if err != nil || !found {
		return ErrUnconfirmed
	}
	return nil
}

func (j *Journal) matches(ctx context.Context, key string, expected []byte) (bool, error) {
	data, err := j.read(ctx, key)
	if err != nil || data == nil {
		return false, err
	}
	if !bytes.Equal(data, expected) {
		return false, ErrUnconfirmed
	}
	return true, nil
}

func (j *Journal) read(ctx context.Context, key string) ([]byte, error) {
	result, err := j.store.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(j.bucket), Key: aws.String(key)})
	if responseStatus(err) == http.StatusNotFound && errorCode(err) == "NoSuchKey" {
		return nil, nil
	}
	if err != nil || result == nil || result.Body == nil {
		return nil, ErrUnconfirmed
	}
	data, readErr := io.ReadAll(io.LimitReader(result.Body, maxRecord+1))
	closeErr := result.Body.Close()
	if readErr != nil || closeErr != nil || len(data) > maxRecord ||
		result.ContentLength == nil || *result.ContentLength != int64(len(data)) {
		return nil, ErrUnconfirmed
	}
	recorded, err := j.envelope.Open(nil, nil, data, []byte(key))
	if err != nil {
		return nil, ErrUnconfirmed
	}
	return recorded, nil
}

func responseStatus(err error) int {
	var response *awsHTTP.ResponseError
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}

func bucketLocked(err error) bool {
	return responseStatus(err) == http.StatusConflict && errorCode(err) == "ObjectLockedByBucketPolicy"
}

func errorCode(err error) string {
	var response smithy.APIError
	if errors.As(err, &response) {
		return response.ErrorCode()
	}
	return ""
}
