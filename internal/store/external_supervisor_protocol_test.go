package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExternalSupervisorRootRotationMatchesFrozenP3FClosedSchema(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "p3f", "fixtures", "independent-supervisor-protocol-adapter-v1.canonical.json"))
	if err != nil {
		t.Fatalf("read P3f fixture: %v", err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatalf("decode P3f fixture: %v", err)
	}
	lifecycle, ok := fixture["trust_root_lifecycle"].(map[string]any)
	if !ok {
		t.Fatal("P3f fixture trust_root_lifecycle is not an object")
	}
	exactFields := map[string]struct{}{
		"cross_signature_reference_hash": {},
		"new_root_id":                    {},
		"new_root_spki_sha256":           {},
		"new_root_valid_from":            {},
		"old_root_id":                    {},
		"old_root_not_after":             {},
		"rotation_hash":                  {},
		"schema_version":                 {},
	}
	for _, rootName := range []string{"release_root", "approval_root", "moa_grant_root"} {
		t.Run(rootName, func(t *testing.T) {
			root, ok := lifecycle[rootName].(map[string]any)
			if !ok {
				t.Fatalf("%s is not an object", rootName)
			}
			rotation, ok := root["rotation"].(map[string]any)
			if !ok {
				t.Fatalf("%s rotation is not an object", rootName)
			}
			actualFields := make(map[string]struct{}, len(rotation))
			for field := range rotation {
				actualFields[field] = struct{}{}
			}
			if !reflect.DeepEqual(actualFields, exactFields) {
				t.Fatalf("%s rotation fields = %v, want exact frozen field set %v", rootName, actualFields, exactFields)
			}
			encoded, err := json.Marshal(rotation)
			if err != nil {
				t.Fatalf("marshal %s rotation: %v", rootName, err)
			}
			var record ExternalSupervisorRootRotation
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&record); err != nil {
				t.Fatalf("decode %s rotation against frozen closed schema: %v", rootName, err)
			}
			sealed, err := SealExternalSupervisorRootRotation(record)
			if err != nil {
				t.Fatalf("seal %s rotation: %v", rootName, err)
			}
			if sealed != record {
				t.Fatalf("%s canonical rotation = %+v, want frozen fixture %+v", rootName, sealed, record)
			}
			remarshaled, err := json.Marshal(record)
			if err != nil {
				t.Fatalf("marshal decoded %s rotation: %v", rootName, err)
			}
			var roundTrip map[string]any
			if err := json.Unmarshal(remarshaled, &roundTrip); err != nil {
				t.Fatalf("decode round-trip %s rotation: %v", rootName, err)
			}
			if !reflect.DeepEqual(roundTrip, rotation) {
				t.Fatalf("%s rotation round-trip changed frozen canonical record: got %v want %v", rootName, roundTrip, rotation)
			}
		})
	}
}

func TestExternalSupervisorAuthorizationIdentifiersUseFrozenP3FSafeOpaqueSemantics(t *testing.T) {
	validHash := "sha256:" + strings.Repeat("a", 64)
	approval := ExternalSupervisorReleaseApproval{
		SchemaVersion: ExternalSupervisorReleaseApprovalSchemaVersion,
		ApprovalID:    "release_approval_001", ApproverRootID: "approval_root_001",
		ApproverKeySPKISHA256: validHash, AttestationHash: validHash, RouteMappingHash: validHash,
		Decision: "approved", IssuedAt: "2026-07-25T00:00:00Z", NotAfter: "2026-07-25T01:00:00Z",
	}
	grant := ExternalSupervisorMoARoleGrant{
		SchemaVersion: ExternalSupervisorMoARoleGrantSchemaVersion,
		GrantID:       "moa_grant_001", GranteeRole: "remote_supervisor_runner", GrantorRootID: "moa_root_001",
		GrantorKeySPKISHA256: validHash, ReleaseApprovalHash: validHash, ReleaseAttestationHash: validHash, RouteMappingHash: validHash,
		IssuedAt: "2026-07-25T00:00:00Z", NotAfter: "2026-07-25T01:00:00Z",
	}
	if _, err := SealExternalSupervisorReleaseApproval(approval); err != nil {
		t.Fatalf("valid safe opaque approval ID rejected: %v", err)
	}
	if _, err := SealExternalSupervisorMoARoleGrant(grant); err != nil {
		t.Fatalf("valid safe opaque grant ID rejected: %v", err)
	}

	for _, identifier := range []string{
		"https_approval_001", "url_approval_001", "authority_approval_001", "credential_approval_001",
		"secret_approval_001", "private_key_approval_001", "key_material_approval_001", "command_payload_001",
		"argv_payload_001", "environment_payload_001", "source_payload_001", "artifact_payload_001",
		"evidence_payload_001", "path_payload_001", "release approval 001",
	} {
		t.Run(identifier, func(t *testing.T) {
			approval.ApprovalID = identifier
			if _, err := SealExternalSupervisorReleaseApproval(approval); !errors.Is(err, ErrExternalSupervisorInvalid) {
				t.Fatalf("forbidden approval ID %q error = %v, want %v", identifier, err, ErrExternalSupervisorInvalid)
			}
			grant.GrantID = identifier
			if _, err := SealExternalSupervisorMoARoleGrant(grant); !errors.Is(err, ErrExternalSupervisorInvalid) {
				t.Fatalf("forbidden grant ID %q error = %v, want %v", identifier, err, ErrExternalSupervisorInvalid)
			}
		})
	}
}
