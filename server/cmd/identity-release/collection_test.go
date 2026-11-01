package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestInstalledStatusBindsSourceAndVersion(t *testing.T) {
	want := sourcePackage{"tzdata-legacy", "1", "tzdata"}
	good := "Package: tzdata-legacy\nSource: tzdata\nVersion: 1\nDescription: retained metadata\n continuation\n"
	if err := checkSourceStatus([]byte(good), want); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(good, "Package: tzdata-legacy", "Package: other", 1),
		strings.Replace(good, "Source: tzdata", "Source: other", 1),
		strings.Replace(good, "Version: 1", "Version: 2", 1),
		strings.Replace(good, "Source: tzdata\n", "", 1),
		good + "Package: duplicate\n", good + "\nVersion: 1\n",
		" orphan\n" + good, good + "\x00", good + "malformed field\n",
	} {
		if err := checkSourceStatus([]byte(bad), want); err == nil {
			t.Fatal("inconsistent installed source package admitted")
		}
	}
	if err := checkSourceStatus([]byte("Package: netbase\nVersion: 6.5\n"), sourcePackage{"netbase", "6.5", "netbase"}); err != nil {
		t.Fatal(err)
	}
}

func TestBaseCollectorNeverAcceptsFailedOrSubstitutedOriginals(t *testing.T) {
	p, err := loadBaseProfile()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"read-error", "short-body", "substituted-body"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			read := func(context.Context, sourceObject) ([]byte, error) {
				calls++
				if kind == "read-error" {
					return nil, fmt.Errorf("read failed")
				}
				if kind == "short-body" {
					return nil, nil
				}
				return []byte("substituted original"), nil
			}
			if _, err := collectBaseOriginals(context.Background(), p, read); err == nil || calls != 1 {
				t.Fatalf("invalid original accepted or acquisition continued: %v calls=%d", err, calls)
			}
		})
	}
}
