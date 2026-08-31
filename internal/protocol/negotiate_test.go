package protocol

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNegotiateIntersection(t *testing.T) {
	v, err := Negotiate([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Errorf("expected highest common version 1, got %d", v)
	}
	if _, err := Negotiate([]int{1}); err != nil {
		t.Errorf("exact match should negotiate: %v", err)
	}
}

// Negotiate must return the HIGHEST common version, not the first match in
// either list. The hub currently supports only [1], which gives the
// selection no real choice, so this test widens the supported set
// temporarily (restored on exit).
func TestNegotiatePicksHighestCommon(t *testing.T) {
	orig := SupportedVersions
	defer func() { SupportedVersions = orig }()
	SupportedVersions = []int{1, 2, 3}

	cases := []struct {
		versions []int
		want     int
		wantErr  bool
	}{
		{[]int{2, 3}, 3, false},    // several common versions → highest
		{[]int{3, 2, 9}, 3, false}, // provider order must not matter; unsupported 9 ignored
		{[]int{1}, 1, false},       // lowest common still negotiates
		{[]int{0, 4}, 0, true},     // nothing in common despite the wider hub set
	}
	for _, tc := range cases {
		v, err := Negotiate(tc.versions)
		if tc.wantErr {
			if err == nil {
				t.Errorf("versions %v: expected error, got %d", tc.versions, v)
			}
			continue
		}
		if err != nil {
			t.Errorf("versions %v: unexpected error: %v", tc.versions, err)
			continue
		}
		if v != tc.want {
			t.Errorf("versions %v: negotiated %d, want %d (highest common)", tc.versions, v, tc.want)
		}
	}
}

func TestNegotiateNoIntersection(t *testing.T) {
	for _, versions := range [][]int{{2, 3}, {}, nil} {
		_, err := Negotiate(versions)
		if err == nil {
			t.Errorf("versions %v: expected error", versions)
			continue
		}
		var verr *VersionError
		if !errors.As(err, &verr) {
			t.Errorf("versions %v: expected *VersionError, got %T", versions, err)
			continue
		}
		// The error must carry the provider's offered versions verbatim and
		// the hub's supported set, so diagnostics can explain the mismatch.
		if fmt.Sprintf("%v", verr.ProviderVersions) != fmt.Sprintf("%v", versions) {
			t.Errorf("versions %v: ProviderVersions = %v", versions, verr.ProviderVersions)
		}
		if fmt.Sprintf("%v", verr.HubVersions) != fmt.Sprintf("%v", SupportedVersions) {
			t.Errorf("HubVersions = %v, want %v", verr.HubVersions, SupportedVersions)
		}
		// The message names both sides.
		if !strings.Contains(verr.Error(), "provider offers") || !strings.Contains(verr.Error(), "hub supports") {
			t.Errorf("error message should describe both sides: %q", verr.Error())
		}
	}
}

// The hub must distinguish a manifest that parses but speaks no common
// version (unsupported_schema_version) from one that fails to parse or
// validate (malformed_manifest). These are different error codes.
func TestErrorCodesDistinct(t *testing.T) {
	if ErrUnsupportedSchemaVersion == ErrMalformedManifest {
		t.Error("unsupported_schema_version and malformed_manifest must be distinct codes")
	}
}
