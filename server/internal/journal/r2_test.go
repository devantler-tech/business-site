package journal

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/devantler-tech/business-site/server/internal/identity"
)

const (
	testAccount = "1234567890abcdef1234567890abcdef"
	testBucket  = "fictional-deletion-journal"
	testIssuer  = "https://example.test/dex"
)

var customer = identity.CustomerDeletionTarget{
	UserID: "fictional-customer", Organization: "fictional-organization",
	CreatedAt: time.Date(2026, time.January, 5, 12, 33, 34, 123456789, time.UTC),
}

type bucketFixture struct {
	t           *testing.T
	mu          sync.Mutex
	objects     map[string][]byte
	duplicate   int
	missingCode string
	created     int
	discardPut  bool
	repeatPage  bool
	reversePage bool
}

func (b *bucketFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	b.t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	reply := httptest.NewRecorder()
	listing := req.URL.Path == "/"+testBucket && req.URL.Query().Get("list-type") == "2"
	if req.URL.Host != testAccount+".eu.r2.cloudflarestorage.com" || req.URL.Scheme != "https" ||
		(!listing && !strings.HasPrefix(req.URL.Path, "/"+testBucket+"/account-deletion/v1/fixture/")) ||
		!strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=fixture-access-key/") {
		b.t.Error("journal request did not use the scoped signed R2 destination")
		reply.WriteHeader(http.StatusForbidden)
		return reply.Result(), nil
	}
	if listing {
		b.list(reply, req)
		return reply.Result(), nil
	}
	if req.Method == http.MethodPut {
		b.put(reply, req)
		return reply.Result(), nil
	}
	if req.Method != http.MethodGet {
		b.t.Error("journal used an unsupported storage operation")
		reply.WriteHeader(http.StatusMethodNotAllowed)
		return reply.Result(), nil
	}
	object, found := b.objects[req.URL.Path]
	if !found {
		code := b.missingCode
		if code == "" {
			code = "NoSuchKey"
		}
		s3Error(reply, http.StatusNotFound, code)
		return reply.Result(), nil
	}
	reply.Header().Set("Content-Length", strconv.Itoa(len(object)))
	_, _ = reply.Write(object)
	return reply.Result(), nil
}

func (b *bucketFixture) list(reply http.ResponseWriter, req *http.Request) {
	prefix := req.URL.Query().Get("prefix")
	if prefix != "account-deletion/v1/fixture/accounts/" || req.Method != http.MethodGet {
		b.t.Error("recovery listed outside its scope")
		reply.WriteHeader(http.StatusBadRequest)
		return
	}
	cursor := req.URL.Query().Get("continuation-token")
	keys := make([]string, 0, len(b.objects))
	for path := range b.objects {
		key := strings.TrimPrefix(path, "/"+testBucket+"/")
		afterCursor := cursor == "" || key > cursor
		if b.reversePage && cursor != "" {
			afterCursor = key < cursor
		}
		if strings.HasPrefix(key, prefix) && (afterCursor || b.repeatPage) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if b.reversePage {
		slices.Reverse(keys)
	}
	var page struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		IsTruncated           bool
		NextContinuationToken string `xml:",omitempty"`
		Contents              []struct{ Key string }
	}
	if len(keys) > 0 {
		page.Contents = append(page.Contents, struct{ Key string }{keys[0]})
	}
	if len(keys) > 1 {
		page.IsTruncated = true
		page.NextContinuationToken = keys[0]
	}
	if err := xml.NewEncoder(reply).Encode(&page); err != nil {
		b.t.Error("fixture could not encode the page")
	}
}

func (b *bucketFixture) put(reply http.ResponseWriter, req *http.Request) {
	if req.Header.Get("If-None-Match") != "*" {
		b.t.Error("journal write did not require a new object")
		reply.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, exists := b.objects[req.URL.Path]; exists {
		if b.duplicate == http.StatusConflict {
			s3Error(reply, http.StatusConflict, "ObjectLockedByBucketPolicy")
		} else {
			s3Error(reply, http.StatusPreconditionFailed, "PreconditionFailed")
		}
		return
	}
	defer func() { _ = req.Body.Close() }()
	object, err := io.ReadAll(req.Body)
	if err != nil {
		b.t.Error("fixture could not read the write")
		reply.WriteHeader(http.StatusBadRequest)
		return
	}
	if !b.discardPut {
		b.objects[req.URL.Path] = object
		b.created++
	}
	reply.WriteHeader(http.StatusOK)
}

func s3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<Error><Code>"+code+"</Code></Error>")
}

func makeFixtureJournal(t *testing.T, transport *bucketFixture) *Journal {
	t.Helper()
	client, err := newR2(testAccount, testBucket, "fixture", [32]byte{1}, [32]byte{2},
		aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "fixture-access-key", SecretAccessKey: "fixture-secret-key"}, nil
		}), transport)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestJournalRequiresBothAuthenticatedRecordsForRecovery(t *testing.T) {
	for _, conflict := range []int{http.StatusConflict, http.StatusPreconditionFailed} {
		t.Run(http.StatusText(conflict), func(t *testing.T) {
			bucket := &bucketFixture{t: t, objects: map[string][]byte{}, duplicate: conflict}
			web, recovery := makeFixtureJournal(t, bucket), makeFixtureJournal(t, bucket)
			if err := web.Register(t.Context()); err != nil {
				t.Fatal(err)
			}
			if entries, err := recovery.RecoveryInventory(t.Context()); err != nil || len(entries) != 0 {
				t.Fatalf("fresh recovery inventory contains %d entries, %v", len(entries), err)
			}
			if err := web.RecordDeletionConfirmation(t.Context(), testIssuer, customer); err == nil {
				t.Fatal("confirmation succeeded before the intent existed")
			}
			var group sync.WaitGroup
			for range 6 {
				group.Go(func() {
					if err := web.RecordDeletionIntent(t.Context(), testIssuer, customer); err != nil {
						t.Error(err)
					}
				})
			}
			group.Wait()
			verifyEntry(t, recovery, Pending)
			for range 2 {
				if err := web.RecordDeletionConfirmation(t.Context(), testIssuer, customer); err != nil {
					t.Fatal(err)
				}
			}
			verifyEntry(t, recovery, Confirmed)
			reused := customer
			reused.CreatedAt = reused.CreatedAt.Add(time.Nanosecond)
			if err := web.RecordDeletionConfirmation(t.Context(), testIssuer, reused); err == nil {
				t.Fatal("changed account creation time received confirmation")
			}
			bucket.mu.Lock()
			if bucket.created != 3 {
				bucket.mu.Unlock()
				t.Fatal("retries changed the original pair")
			}
			for key, data := range bucket.objects {
				if strings.Contains(key, customer.UserID) || bytes.Contains(data, []byte(customer.UserID)) {
					bucket.mu.Unlock()
					t.Fatal("independent storage exposed the native account identifier")
				}
				if strings.HasSuffix(key, intent) {
					bucket.objects[strings.TrimSuffix(key, intent)+confirmed] = bytes.Clone(data)
				}
			}
			bucket.mu.Unlock()
			if entries, err := recovery.RecoveryInventory(t.Context()); err == nil || entries != nil {
				t.Fatal("an intent substituted for confirmation")
			}
		})
	}
}

func verifyEntry(t *testing.T, recovery *Journal, state State) {
	t.Helper()
	entries, err := recovery.RecoveryInventory(t.Context())
	if err != nil || len(entries) != 1 || entries[0] != (RecoveryEntry{Issuer: testIssuer, Target: customer, State: state}) {
		t.Fatalf("recovery expected one entry in state %v, found %d, error %v", state, len(entries), err)
	}
}

func TestJournalRejectsFalseAcknowledgmentAndMissingBucket(t *testing.T) {
	bucket := &bucketFixture{t: t, objects: map[string][]byte{}}
	web := makeFixtureJournal(t, bucket)
	if err := web.Register(t.Context()); err != nil {
		t.Fatal(err)
	}
	bucket.mu.Lock()
	bucket.discardPut = true
	bucket.mu.Unlock()
	if err := web.RecordDeletionIntent(t.Context(), testIssuer, customer); err == nil {
		t.Fatal("a successful write response without readback admitted provider removal")
	}
	bucket.mu.Lock()
	bucket.missingCode = "NoSuchBucket"
	key, _ := web.registration()
	delete(bucket.objects, "/"+testBucket+"/"+key)
	bucket.mu.Unlock()
	if entries, err := web.RecoveryInventory(t.Context()); err == nil || entries != nil {
		t.Fatal("missing independent storage allowed recovery")
	}
}

func TestRecoveryInventoryRequiresCompleteAuthenticPages(t *testing.T) {
	bucket := &bucketFixture{t: t, objects: map[string][]byte{}}
	web, recovery := makeFixtureJournal(t, bucket), makeFixtureJournal(t, bucket)
	if err := web.Register(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending := customer
	pending.UserID = "fictional-pending"
	if err := web.RecordDeletionIntent(t.Context(), testIssuer, customer); err != nil {
		t.Fatal(err)
	}
	if err := web.RecordDeletionIntent(t.Context(), testIssuer, pending); err != nil {
		t.Fatal(err)
	}
	if err := web.RecordDeletionConfirmation(t.Context(), testIssuer, customer); err != nil {
		t.Fatal(err)
	}
	entries, err := recovery.RecoveryInventory(t.Context())
	if err != nil || len(entries) != 2 {
		t.Fatalf("complete inventory returned %d accounts, %v", len(entries), err)
	}
	statuses := map[identity.CustomerDeletionTarget]State{}
	for _, item := range entries {
		if item.Issuer != testIssuer {
			t.Fatal("recovery inventory changed the issuer")
		}
		statuses[item.Target] = item.State
	}
	if statuses[customer] != Confirmed || statuses[pending] != Pending {
		t.Fatal("recovery inventory did not distinguish a confirmed removal from a pending account")
	}
	bucket.mu.Lock()
	bucket.reversePage = true
	bucket.mu.Unlock()
	if reversed, err := recovery.RecoveryInventory(t.Context()); err != nil || !slices.Equal(reversed, entries) {
		t.Fatal("recovery inventory depended on page order")
	}
	bucket.mu.Lock()
	bucket.reversePage = false
	bucket.repeatPage = true
	bucket.mu.Unlock()
	if entries, err := recovery.RecoveryInventory(t.Context()); err == nil || entries != nil {
		t.Fatal("an incomplete listing exposed a partial replay inventory")
	}
	bucket.mu.Lock()
	bucket.repeatPage = false
	base, _, _ := web.binding(testIssuer, customer)
	delete(bucket.objects, "/"+testBucket+"/"+base+intent)
	bucket.mu.Unlock()
	entries, err = recovery.RecoveryInventory(t.Context())
	if err != nil || len(entries) != 2 {
		t.Fatal("expiration of the older intent lost a verified removal")
	}
	for _, item := range entries {
		if item.Target == customer && item.State != Confirmed {
			t.Fatal("expiration of the older intent changed a verified removal")
		}
	}
}

func TestRecoveryInventoryRequiresTheRegisteredIndex(t *testing.T) {
	bucket := &bucketFixture{t: t, objects: map[string][]byte{}}
	web := makeFixtureJournal(t, bucket)
	if entries, err := web.RecoveryInventory(t.Context()); err == nil || entries != nil {
		t.Fatal("recovery treated an unregistered store as empty")
	}
	if err := web.RecordDeletionIntent(t.Context(), testIssuer, customer); err == nil {
		t.Fatal("an unregistered store admitted provider removal")
	}
	if err := web.Register(t.Context()); err != nil {
		t.Fatal(err)
	}
	withOtherIndex := *web
	withOtherIndex.indexKey = [32]byte{4}
	if entries, err := withOtherIndex.RecoveryInventory(t.Context()); err == nil || entries != nil {
		t.Fatal("recovery accepted a different index key")
	}
	if err := withOtherIndex.Register(t.Context()); err == nil {
		t.Fatal("a different index key replaced the registered one")
	}
	if entries, err := web.RecoveryInventory(t.Context()); err != nil || len(entries) != 0 {
		t.Fatal("the original scope did not verify legitimate empty recovery")
	}
}
