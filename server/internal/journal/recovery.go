package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/devantler-tech/business-site/server/internal/identity"
)

// RecoveryEntry describes a confirmed account removal or an account awaiting resolution.
type RecoveryEntry struct {
	Issuer string
	Target identity.CustomerDeletionTarget
	State  State
}

type recoveryPair struct {
	entry             RecoveryEntry
	intent, confirmed bool
}

// RecoveryInventory reads every scoped page and authenticates each record.
// The restore process holds account mutations and sign-in throughout recovery.
func (j *Journal) RecoveryInventory(ctx context.Context) ([]RecoveryEntry, error) {
	if err := j.verifyRegistration(ctx); err != nil {
		return nil, err
	}
	input := &s3.ListObjectsV2Input{Bucket: aws.String(j.bucket), Prefix: aws.String(j.prefix())}
	pairs := make(map[string]recoveryPair)
	tokens := make(map[string]bool)
	for {
		page, err := j.store.ListObjectsV2(ctx, input)
		if err != nil || page == nil || page.IsTruncated == nil {
			return nil, ErrUnconfirmed
		}
		for _, item := range page.Contents {
			key := aws.ToString(item.Key)
			base, entry, err := j.readEntry(ctx, key)
			if err != nil {
				return nil, err
			}
			pair := pairs[base]
			isIntent := key == base+intent
			if (isIntent && pair.intent) || (!isIntent && pair.confirmed) {
				return nil, ErrUnconfirmed
			}
			pair.entry = entry
			pair.intent = pair.intent || isIntent
			pair.confirmed = pair.confirmed || !isIntent
			pairs[base] = pair
		}
		next := aws.ToString(page.NextContinuationToken)
		if !*page.IsTruncated && next == "" {
			return recoveryEntries(pairs), nil
		}
		if !*page.IsTruncated || next == "" || tokens[next] || len(page.Contents) == 0 {
			return nil, ErrUnconfirmed
		}
		tokens[next] = true
		input.ContinuationToken = page.NextContinuationToken
	}
}

func recoveryEntries(pairs map[string]recoveryPair) []RecoveryEntry {
	entries := make([]RecoveryEntry, 0, len(pairs))
	for _, key := range slices.Sorted(maps.Keys(pairs)) {
		pair := pairs[key]
		// The authenticated confirmation survives expiration of an older intent.
		if pair.confirmed {
			pair.entry.State = Confirmed
		}
		entries = append(entries, pair.entry)
	}
	return entries
}

func (j *Journal) readEntry(ctx context.Context, key string) (string, RecoveryEntry, error) {
	if !strings.HasPrefix(key, j.prefix()) ||
		(!strings.HasSuffix(key, intent) && !strings.HasSuffix(key, confirmed)) ||
		len(key) > len(j.prefix())+64+len(confirmed) {
		return "", RecoveryEntry{}, ErrUnconfirmed
	}
	data, err := j.read(ctx, key)
	var item record
	if err != nil || json.Unmarshal(data, &item) != nil {
		return "", RecoveryEntry{}, ErrUnconfirmed
	}
	base, expected, err := j.binding(item.Issuer, item.Target)
	if err != nil || !bytes.Equal(data, expected) || (key != base+intent && key != base+confirmed) {
		return "", RecoveryEntry{}, ErrUnconfirmed
	}
	return base, RecoveryEntry{Issuer: item.Issuer, Target: item.Target, State: Pending}, nil
}
