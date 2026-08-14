package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sc-re/sc-capture/internal/client"
	"github.com/sc-re/sc-capture/internal/flow"
)

// SchemaVersion of the on-disk bundle format.
//
// Readers must check MAJOR before anything else, and refuse an unknown MAJOR
// with an explicit diagnostic rather than attempting a partial read (FR-027).
const SchemaVersion = "1.0"

// MetadataFile is the sidecar's name.
const MetadataFile = "session.json"

// Termination states. A session in any of these is valid and verifiable; only
// "clean" carries utc_end and a complete SHA256SUMS.
const (
	TerminationClean       = "clean"
	TerminationInterrupted = "interrupted"
	TerminationDiskFloor   = "disk_floor"
	TerminationError       = "error"
)

// Capture modes. Passive is the default (Principle IV).
const (
	ModePassive = "passive"
	ModeRelay   = "relay"
)

type Software struct {
	Name                  string `json:"name"`
	Version               string `json:"version"`
	GitCommit             string `json:"git_commit,omitempty"`
	ProtocolTablesVersion string `json:"protocol_tables_version,omitempty"`
}

type ClockInfo struct {
	NTPSource string   `json:"ntp_source,omitempty"`
	Method    string   `json:"method,omitempty"`
	Anchors   []Anchor `json:"anchors"`
}

type InterfaceRecord struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	PcapngID  int    `json:"pcapng_interface_id"`
	Offloads  string `json:"offloads_enabled,omitempty"`
	NoCarrier bool   `json:"no_carrier,omitempty"`
}

type Host struct {
	Interfaces      []InterfaceRecord `json:"interfaces"`
	NetnsIsolated   bool              `json:"netns_isolated"`
	CaptureTool     string            `json:"capture_tool"`
	CaptureFilter   string            `json:"capture_filter"`
	Snaplen         int               `json:"snaplen"`
	PacketsCaptured uint64            `json:"packets_captured"`
	PacketsDropped  uint64            `json:"packets_dropped"`
	Platform        string            `json:"platform,omitempty"`
}

// Client identifies the game build a session was captured against.
//
// Populated automatically from the Steam app manifest and the client binary
// itself. A recording without its matching build is frequently undecodable, so
// this is not decoration — it is what lets somebody years from now confirm they
// are looking at the same executable that produced these bytes.
type Client = client.Info

type Rewrite struct {
	Kind        string `json:"kind"`
	MessageType string `json:"message_type"`
	From        string `json:"from"`
	To          string `json:"to"`
	Rationale   string `json:"rationale,omitempty"`
}

type MarkerSummary struct {
	BeaconPort int `json:"beacon_port,omitempty"`
	Count      int `json:"count"`
	FirstSeq   int `json:"first_seq,omitempty"`
	LastSeq    int `json:"last_seq,omitempty"`
}

type SegmentRecord struct {
	File         string     `json:"file"`
	FirstFrameAt *time.Time `json:"first_frame_utc"`
	LastFrameAt  *time.Time `json:"last_frame_utc"`
	Frames       uint64     `json:"frames"`
}

// Metadata is session.json.
//
// This is the file that makes a bundle interpretable by someone who was not
// present and who cannot ask (Principle V). It is written at session start —
// not only at close — so that an abruptly terminated session is still
// self-describing.
type Metadata struct {
	SchemaVersion string `json:"schema_version"`
	BundleID      string `json:"bundle_id"`
	ScenarioID    string `json:"scenario_id,omitempty"`
	VolunteerID   string `json:"volunteer_id,omitempty"`
	Region        string `json:"region"`

	Software Software `json:"software"`
	Client   *Client  `json:"client,omitempty"`
	Host     Host     `json:"host"`

	UTCStart time.Time  `json:"utc_start"`
	UTCEnd   *time.Time `json:"utc_end"`

	Clock ClockInfo `json:"clock"`

	Mode     string    `json:"mode"`
	Rewrites []Rewrite `json:"rewrites"`

	ServicesObserved []string          `json:"services_observed"`
	Connections      []flow.Connection `json:"connections,omitempty"`

	Sensitive         bool   `json:"sensitive"`
	SensitivityReason string `json:"sensitivity_reason"`
	CredentialWarning bool   `json:"credential_warning"`

	Termination string          `json:"termination"`
	Segments    []SegmentRecord `json:"segments"`
	Markers     MarkerSummary   `json:"markers"`

	Anomalies []string `json:"anomalies"`
	Notes     string   `json:"notes,omitempty"`

	mu  sync.Mutex
	dir string
}

// SensitivityReason is stated plainly rather than left to the contributor to
// infer. The master-server protocol has no transport encryption, so
// authentication material is in the journal in the clear.
const SensitivityReason = "Contains raw game traffic. The master-server protocol has no transport " +
	"encryption, so this session may include login credentials, session tokens and account " +
	"identifiers in the clear. Treat it as you would a password file."

// NewMetadata builds the initial sidecar for a session.
func NewMetadata(dir, bundleID string, sw Software, start time.Time) *Metadata {
	return &Metadata{
		SchemaVersion:     SchemaVersion,
		BundleID:          bundleID,
		Software:          sw,
		UTCStart:          start.UTC(),
		Mode:              ModePassive,
		Rewrites:          []Rewrite{},
		ServicesObserved:  []string{},
		Sensitive:         true,
		SensitivityReason: SensitivityReason,
		Termination:       TerminationInterrupted, // until proven otherwise
		Segments:          []SegmentRecord{},
		Anomalies:         []string{},
		dir:               dir,
	}
}

// Anomaly records something unexpected, for the reader who was not there.
func (m *Metadata) Anomaly(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Anomalies = append(m.Anomalies, fmt.Sprintf(format, args...))
}

// Write persists the sidecar atomically.
//
// Called at session start, on every periodic clock anchor, and at close. The
// early and repeated writes are what make an interrupted session
// self-describing rather than an anonymous pile of frames.
func (m *Metadata) Write() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := filepath.Join(m.dir, MetadataFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, MetadataFile))
}

// LoadMetadata reads a sidecar and enforces the version rule.
//
// An unknown MAJOR is refused outright: a partial read of a format we do not
// understand is how an archive gets silently misinterpreted, which is exactly
// what Principle V exists to prevent.
func LoadMetadata(dir string) (*Metadata, error) {
	path := filepath.Join(dir, MetadataFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Read the version before anything else, including before full unmarshal,
	// so a future format cannot trip us on a field we misparse.
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", MetadataFile, err)
	}
	if err := CheckSchemaVersion(probe.SchemaVersion); err != nil {
		return nil, err
	}

	var m Metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m.dir = dir
	return &m, nil
}

// ErrSchemaUnreadable reports a bundle written by an incompatible future
// version.
type ErrSchemaUnreadable struct {
	Found    string
	Supports string
}

func (e *ErrSchemaUnreadable) Error() string {
	return fmt.Sprintf("bundle declares schema version %s; this build understands %s. "+
		"Refusing to read it — a partial read of an unknown format is how an archive gets "+
		"silently misinterpreted. Use a build that supports %s.", e.Found, e.Supports, e.Found)
}

// CheckSchemaVersion accepts a known MAJOR and refuses anything else.
func CheckSchemaVersion(v string) error {
	if v == "" {
		return &ErrSchemaUnreadable{Found: "(absent)", Supports: SchemaVersion}
	}
	major := func(s string) int {
		n, err := strconv.Atoi(strings.SplitN(s, ".", 2)[0])
		if err != nil {
			return -1
		}
		return n
	}
	got, want := major(v), major(SchemaVersion)
	if got < 0 || got != want {
		return &ErrSchemaUnreadable{Found: v, Supports: SchemaVersion}
	}
	return nil
}
