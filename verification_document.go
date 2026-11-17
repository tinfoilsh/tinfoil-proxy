package main

import (
	"time"

	"github.com/tinfoilsh/tinfoil-go/verify"
	"github.com/tinfoilsh/tinfoil-go/verify/document"
	"github.com/tinfoilsh/tinfoil-go/verify/measurement"
)

// The measurement types reported in the verification document.
const (
	sevSNPMeasurementType = "https://tinfoil.sh/predicate/sev-snp-guest/v2"
	tdxMeasurementType    = "https://tinfoil.sh/predicate/tdx-guest/v2"
)

type softwareIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// documentMeasurement is a measurement type and its registers in platform
// order, lowercase hex, with "" for a register the release leaves
// unconstrained.
type documentMeasurement struct {
	Type      string   `json:"type"`
	Registers []string `json:"registers"`
}

// VerifiedDocument is the verification document the proxy publishes on
// /verification-document and to the desktop app. It is a snapshot of the
// SDK's verification, which has no JSON form of its own.
type VerifiedDocument struct {
	ConfigRepo         string
	EnclaveHost        string
	Verifier           softwareIdentity
	VerifiedAt         string
	CodeDigest         string
	CodeTag            string
	CodeMeasurement    *documentMeasurement
	EnclaveMeasurement *documentMeasurement
	CryptoMaterial     []document.CryptoMaterialItem
	FreshnessExpiresAt time.Time
}

func newVerifiedDocument(enclaveHost string, verification *verify.Verification) *VerifiedDocument {
	if verification == nil {
		return nil
	}
	metadata := verification.Metadata()
	return &VerifiedDocument{
		ConfigRepo:  verification.ConfigRepo(),
		EnclaveHost: enclaveHost,
		Verifier: softwareIdentity{
			Name:    metadata.Verifier.Name,
			Version: metadata.Verifier.Version,
		},
		VerifiedAt:         metadata.VerifiedAt.UTC().Format(time.RFC3339Nano),
		CodeDigest:         verification.CodeDigest(),
		CodeTag:            verification.CodeTag(),
		CodeMeasurement:    expectedMeasurementDocument(verification.CodeMeasurement()),
		EnclaveMeasurement: enclaveMeasurementDocument(verification.EnclaveMeasurement()),
		CryptoMaterial:     verification.CryptoMaterial(),
		FreshnessExpiresAt: verification.FreshnessExpiresAt(),
	}
}

func (d *VerifiedDocument) TLSPublicKeyFP() (string, bool) {
	return verify.CryptoMaterialData(d.CryptoMaterial, document.CryptoMaterialIDTLS, document.KeySPKIFPSHA256V1Format)
}

func (d *VerifiedDocument) HPKEPublicKey() (string, bool) {
	return verify.CryptoMaterialData(d.CryptoMaterial, document.CryptoMaterialIDHPKE, document.KeyX25519HPKEV1Format)
}

func enclaveMeasurementDocument(measured measurement.Measurement) *documentMeasurement {
	if measured == nil {
		return nil
	}
	return expectedMeasurementDocument(measured.Expected())
}

func expectedMeasurementDocument(expected measurement.Expected) *documentMeasurement {
	switch e := expected.(type) {
	case measurement.ExpectedSEVSNP:
		return &documentMeasurement{Type: sevSNPMeasurementType, Registers: registerStrings(e.Launch)}
	case measurement.ExpectedTDX:
		return &documentMeasurement{Type: tdxMeasurementType, Registers: registerStrings(e.MRTD, e.RTMR0, e.RTMR1, e.RTMR2, e.RTMR3)}
	}
	return nil
}

func registerStrings(registers ...*measurement.Register) []string {
	out := make([]string, len(registers))
	for i, register := range registers {
		if register != nil {
			out[i] = register.String()
		}
	}
	return out
}
