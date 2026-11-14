package types

import "encoding/json"

type ContractExtractionStatus string

const (
	ContractExtractionStatusPending    ContractExtractionStatus = "pending"
	ContractExtractionStatusProcessing ContractExtractionStatus = "processing"
	ContractExtractionStatusCompleted  ContractExtractionStatus = "completed"
	ContractExtractionStatusFailed     ContractExtractionStatus = "failed"
)

type ContractDocument struct {
	Key       string `json:"key"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// ContractExtraction is the stored extraction JSON. It is opaque so Ent stores it unchanged
// and generates no slice-append API for it.
type ContractExtraction struct {
	raw json.RawMessage
}

func NewContractExtraction(raw json.RawMessage) *ContractExtraction {
	if len(raw) == 0 {
		return nil
	}
	return &ContractExtraction{raw: raw}
}

func (e *ContractExtraction) Raw() json.RawMessage {
	if e == nil {
		return nil
	}
	return e.raw
}

func (e ContractExtraction) MarshalJSON() ([]byte, error) {
	if len(e.raw) == 0 {
		return []byte("null"), nil
	}
	return e.raw, nil
}

func (e *ContractExtraction) UnmarshalJSON(data []byte) error {
	e.raw = append(json.RawMessage(nil), data...)
	return nil
}
