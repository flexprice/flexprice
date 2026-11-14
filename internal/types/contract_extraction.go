package types

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
