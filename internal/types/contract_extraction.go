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

// ContractJSON is a stored JSON document on a contract (the extraction or the review). It is opaque
// so Ent stores it unchanged and generates no slice-append API for it.
type ContractJSON struct {
	raw json.RawMessage
}

func NewContractJSON(raw json.RawMessage) *ContractJSON {
	if len(raw) == 0 {
		return nil
	}
	return &ContractJSON{raw: raw}
}

func (e *ContractJSON) Raw() json.RawMessage {
	if e == nil {
		return nil
	}
	return e.raw
}

func (e ContractJSON) MarshalJSON() ([]byte, error) {
	if len(e.raw) == 0 {
		return []byte("null"), nil
	}
	return e.raw, nil
}

func (e *ContractJSON) UnmarshalJSON(data []byte) error {
	e.raw = append(json.RawMessage(nil), data...)
	return nil
}
