package entity

import "time"

// Certificate contains only data approved for the public API.
type Certificate struct {
	ID              string    `json:"id"`
	IMEI            string    `json:"imei"`
	Number          string    `json:"number"`
	Status          string    `json:"status"`
	SuccessorNumber *string   `json:"successor_number"`
	IssuedAt        time.Time `json:"issued_at"`
	IsDemo          bool      `json:"is_demo"`
	Snapshot        Snapshot  `json:"snapshot"`
	Listing         *Listing  `json:"listing"`
}

type Snapshot struct {
	SchemaVersion      int                 `json:"schema_version"`
	Device             DeviceSpec          `json:"device"`
	Grade              Grade               `json:"grade"`
	Recording          Recording           `json:"recording"`
	Battery            Battery             `json:"battery"`
	OSHistory          []OSVersion         `json:"os_history"`
	StatusHistory      []StatusRecord      `json:"status_history"`
	MaintenanceRecords []MaintenanceRecord `json:"maintenance_records"`
}

type DeviceSpec struct {
	Manufacturer     string    `json:"manufacturer"`
	Model            string    `json:"model"`
	DeviceName       *string   `json:"device_name"`
	SoC              *string   `json:"soc"`
	CPUCores         *int      `json:"cpu_cores"`
	RAMBytes         *int64    `json:"ram_bytes"`
	StorageBytes     *int64    `json:"storage_bytes"`
	ScreenSizeInch   *float64  `json:"screen_size_inch"`
	ScreenDensityDPI *int      `json:"screen_density_dpi"`
	OSVersion        *string   `json:"os_version"`
	SDKInt           *int      `json:"sdk_int"`
	CollectedAt      time.Time `json:"collected_at"`
}

type Grade struct {
	Value            *string     `json:"value"`
	Label            string      `json:"label"`
	Explanation      string      `json:"explanation"`
	AlgorithmVersion string      `json:"algorithm_version"`
	Items            []GradeItem `json:"items"`
}

type GradeItem struct {
	Name        string  `json:"name"`
	Value       *string `json:"value"`
	Explanation string  `json:"explanation"`
}

type Recording struct {
	StartedAt           time.Time      `json:"started_at"`
	EndedAt             time.Time      `json:"ended_at"`
	Count               int            `json:"count"`
	GapThresholdSeconds *int64         `json:"gap_threshold_seconds"`
	Gaps                []RecordingGap `json:"gaps"`
}

type RecordingGap struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type Battery struct {
	LatestFullChargeMAh  *int           `json:"latest_full_charge_mah"`
	InitialFullChargeMAh *int           `json:"initial_full_charge_mah"`
	ChangePercent        *float64       `json:"change_percent"`
	CycleCount           *int           `json:"cycle_count"`
	Trend                []BatteryPoint `json:"trend"`
}

type BatteryPoint struct {
	CollectedAt   time.Time `json:"collected_at"`
	FullChargeMAh *int      `json:"full_charge_mah"`
	TemperatureC  *float64  `json:"temperature_c"`
}

type OSVersion struct {
	CollectedAt time.Time `json:"collected_at"`
	OSVersion   string    `json:"os_version"`
	SDKInt      int       `json:"sdk_int"`
}

type StatusRecord struct {
	Sequence             int64     `json:"sequence"`
	CollectedAt          time.Time `json:"collected_at"`
	OSVersion            string    `json:"os_version"`
	SDKInt               int       `json:"sdk_int"`
	BatteryTempC         *float64  `json:"battery_temp_c"`
	BatteryCycleCount    *int      `json:"battery_cycle_count"`
	BatteryChargeMAh     *int      `json:"battery_charge_mah"`
	BatteryFullChargeMAh *int      `json:"battery_full_charge_mah"`
	BootCount            *int      `json:"boot_count"`
	HadAbnormalExit      *bool     `json:"had_abnormal_exit"`
	ThermalHeadroom      *float64  `json:"thermal_headroom"`
}

type MaintenanceRecord struct {
	ProviderName string    `json:"provider_name"`
	ProviderType string    `json:"provider_type"`
	RecordedAt   time.Time `json:"recorded_at"`
	Description  string    `json:"description"`
}

type Listing struct {
	Marketplace  string    `json:"marketplace"`
	ItemID       string    `json:"item_id"`
	IMEISuffix   *string   `json:"imei_suffix"`
	RegisteredAt time.Time `json:"registered_at"`
}

// VerificationRecord preserves canonical bytes as a string, never re-encoded JSONB.
type VerificationRecord struct {
	Sequence           int64       `json:"sequence"`
	Type               string      `json:"type"`
	CollectedAt        time.Time   `json:"collected_at"`
	CanonicalPayload   string      `json:"canonical_payload"`
	PayloadHash        string      `json:"payload_hash"`
	PreviousHash       *string     `json:"previous_hash"`
	SignatureBase64    string      `json:"signature_base64"`
	PublicKey          string      `json:"public_key"`
	SignatureAlgorithm string      `json:"signature_algorithm"`
	Canonicalization   string      `json:"canonicalization"`
	AnchorState        string      `json:"anchor_state"`
	Chain              *string     `json:"chain"`
	TransactionHash    *string     `json:"transaction_hash"`
	MerkleRoot         *string     `json:"merkle_root"`
	LeafIndex          *int        `json:"leaf_index"`
	MerkleProof        []ProofStep `json:"merkle_proof"`
}

type ProofStep struct {
	Side string `json:"side"`
	Hash string `json:"hash"`
}
