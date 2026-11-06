package entity

import "time"

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
