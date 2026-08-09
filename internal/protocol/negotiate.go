package protocol

import "fmt"

// VersionError reports a failed version negotiation. It is distinct from a
// malformed manifest: the manifest parsed fine, but speaks no protocol
// version the hub supports.
type VersionError struct {
	ProviderVersions []int
	HubVersions      []int
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("no common kg.provider/v1 version: provider offers %v, hub supports %v",
		e.ProviderVersions, e.HubVersions)
}

// Negotiate intersects the hub's supported versions with the provider's
// declared protocol_versions and returns the highest common version.
func Negotiate(providerVersions []int) (int, error) {
	best := -1
	for _, hv := range SupportedVersions {
		for _, pv := range providerVersions {
			if hv == pv && hv > best {
				best = hv
			}
		}
	}
	if best < 0 {
		return 0, &VersionError{ProviderVersions: providerVersions, HubVersions: SupportedVersions}
	}
	return best, nil
}
