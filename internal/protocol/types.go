// Package protocol defines the kg.provider/v1 (describe/available) and
// kg.execution/v1 (invoke envelope) wire contracts consumed by the hub.
//
// The hub never invents provider options: everything about a capability
// (flags, enums, input schema) comes from the provider's self-description.
package protocol

import "encoding/json"

// Protocol identifiers.
const (
	ProviderProtocol  = "kg.provider/v1"
	ExecutionProtocol = "kg.execution/v1"

	// PipelineProtocol identifies a declarative pipeline definition file.
	PipelineProtocol = "kg.pipeline/v1"
	// PipelineExecutionProtocol identifies the pipeline runner's stdout
	// envelope (one per `kg pipeline run` invocation).
	PipelineExecutionProtocol = "kg.pipeline.execution/v1"
)

// SupportedVersions is the set of kg.provider/v1 protocol versions the hub
// understands. Version negotiation intersects this with the provider's
// declared protocol_versions.
var SupportedVersions = []int{1}

// Manifest is the output of `<provider> describe --json`.
type Manifest struct {
	Protocol         string       `json:"protocol"`
	ProtocolVersions []int        `json:"protocol_versions"`
	Provider         ProviderInfo `json:"provider"`
	Source           *SourceInfo  `json:"source,omitempty"`
	Capabilities     []Capability `json:"capabilities"`
}

// SourceInfo points maintainers from capability help to the provider checkout.
type SourceInfo struct {
	LocalCodePath string `json:"local_code_path,omitempty"`
}

// ProviderInfo identifies a provider binary.
type ProviderInfo struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Capability is one self-described capability entry in a manifest.
type Capability struct {
	CapabilityID string          `json:"capability_id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	SideEffects  []string        `json:"side_effects"`
	InputSchema  json.RawMessage `json:"input_schema"`
	Output       OutputSpec      `json:"output"`
	CLISpec      CLISpec         `json:"cli_spec"`
}

// OutputSpec declares how a capability returns data.
type OutputSpec struct {
	// Mode is "result-json" (inline JSON in the envelope result) or
	// "artifact" (files referenced from the envelope artifacts list).
	Mode string `json:"mode"`
	// Kind is the payload kind: "kg-document", "chunks", "communities", "json".
	Kind string `json:"kind"`
}

// CLISpec describes how to render an argv vector for a capability.
// Emission order is: Always ++ Subcommand ++ Positionals ++ Flags
// (flags sorted by Order, tiebreak on Flag).
type CLISpec struct {
	Subcommand  []string         `json:"subcommand"`
	Always      []string         `json:"always"`
	Positionals []PositionalSpec `json:"positionals"`
	Flags       []FlagSpec       `json:"flags"`
}

// PositionalSpec binds a positional argv slot to an input property name.
type PositionalSpec struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

// Flag kinds.
const (
	FlagString  = "string"
	FlagNumber  = "number"
	FlagBoolean = "boolean"
	FlagArray   = "array"
)

// FlagSpec describes one CLI flag derived from an input property.
type FlagSpec struct {
	Name  string `json:"name"` // input property name
	Flag  string `json:"flag"` // argv token, e.g. "-b" or "--coref"
	Kind  string `json:"kind"` // string|number|boolean|array
	Order int    `json:"order,omitempty"`

	Optional bool `json:"optional,omitempty"`
	Default  any  `json:"default,omitempty"`

	// Repeatable: array flag emitted once per element.
	Repeatable bool `json:"repeatable,omitempty"`
	// Join: array flag emitted once with elements joined by this separator.
	Join string `json:"join,omitempty"`

	// Stdout marks the flag whose value selects the output artifact path.
	Stdout bool `json:"stdout,omitempty"`

	// Negated booleans emit the flag when the value is false
	// (plain booleans emit only when true).
	Negated bool `json:"negated,omitempty"`
}

// AvailableReport is the output of `<provider> available --json` (exit 0).
type AvailableReport struct {
	Available bool            `json:"available"`
	Ready     []AvailableItem `json:"ready"`
	Missing   []AvailableItem `json:"missing"`
	CacheDir  string          `json:"cache_dir,omitempty"`
}

// AvailableItem names one ready or missing dependency.
type AvailableItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Envelope is the single stdout payload of an invocation
// (`<provider> invoke <capability_id> --request -`, or a hub-wrapped
// fallback execution). Logs always go to stderr, never stdout.
type Envelope struct {
	Protocol     string       `json:"protocol"`
	CapabilityID string       `json:"capability_id"`
	Provider     string       `json:"provider"`
	Status       string       `json:"status"` // "ok" | "error"
	Result       any          `json:"result,omitempty"`
	Artifacts    []Artifact   `json:"artifacts,omitempty"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
	Error        *ErrorInfo   `json:"error,omitempty"`
}

// Artifact references one output file.
type Artifact struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Checksum string `json:"checksum,omitempty"`
}

// Diagnostic is a non-fatal note attached to an envelope.
type Diagnostic struct {
	Severity string `json:"severity"` // "info" | "warning" | "error"
	Message  string `json:"message"`
}

// ErrorInfo carries a machine-readable failure.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Hub error codes.
const (
	ErrUnsupportedSchemaVersion = "unsupported_schema_version"
	ErrMalformedManifest        = "malformed_manifest"
	ErrCapabilityNotFound       = "capability_not_found"
	ErrProviderNotFound         = "provider_not_found"
	ErrPolicyDenied             = "policy_denied"
	ErrInvalidInput             = "invalid_input"
	ErrInvocationFailed         = "invocation_failed"

	// Pipeline runner error codes.
	ErrInvalidPipeline       = "invalid_pipeline"
	ErrIncompatibleStageEdge = "incompatible_stage_edge"
)

// NewEnvelope builds an envelope skeleton with the protocol field set.
func NewEnvelope(capabilityID, provider string) *Envelope {
	return &Envelope{
		Protocol:     ExecutionProtocol,
		CapabilityID: capabilityID,
		Provider:     provider,
	}
}

// ErrorEnvelope builds a failed envelope carrying code+message.
func ErrorEnvelope(capabilityID, provider, code, message string) *Envelope {
	env := NewEnvelope(capabilityID, provider)
	env.Status = "error"
	env.Error = &ErrorInfo{Code: code, Message: message}
	return env
}
