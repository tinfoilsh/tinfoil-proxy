package main

import (
	"time"

	"github.com/tinfoilsh/tinfoil-go/verify"
	"github.com/tinfoilsh/tinfoil-go/verify/measurement"
)

const (
	platformSEVSNP = "sev-snp"
	platformTDX    = "tdx"
)

// verificationSummary is what the proxy reports about the enclave it verified,
// on /verification-document and to the desktop app. Every field is a string,
// so two summaries compare with ==.
type verificationSummary struct {
	Enclave           string `json:"enclave"`
	Repo              string `json:"repo"`
	Project           string `json:"project"`
	ReleaseTag        string `json:"release_tag"`
	ReleaseDigest     string `json:"release_digest"`
	Platform          string `json:"platform"`
	TLSKeyFingerprint string `json:"tls_key_fingerprint"`
	VerifiedAt        string `json:"verified_at"`
	ExpiresAt         string `json:"expires_at"`
}

func newVerificationSummary(enclave string, verification *verify.Verification) *verificationSummary {
	if verification == nil {
		return nil
	}
	fingerprint, _ := verification.TLSPublicKeyFP()
	// A config registry project is reported apart from the GitHub repository
	// of the code or runtime it runs.
	config, _ := verification.Config()
	return &verificationSummary{
		Enclave:           enclave,
		Repo:              verification.ConfigRepo(),
		Project:           config.Name,
		ReleaseTag:        verification.CodeTag(),
		ReleaseDigest:     verification.CodeDigest(),
		Platform:          platformName(verification.EnclaveMeasurement()),
		TLSKeyFingerprint: fingerprint,
		VerifiedAt:        verification.Metadata().VerifiedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:         verification.FreshnessExpiresAt().UTC().Format(time.RFC3339Nano),
	}
}

func platformName(measured measurement.Measurement) string {
	switch measured.(type) {
	case measurement.SEVSNP:
		return platformSEVSNP
	case measurement.TDX:
		return platformTDX
	}
	return ""
}
