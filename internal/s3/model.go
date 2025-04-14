package s3

type Document struct {
	ID   string       `json:"id"`
	Data []byte       `json:"data"`
	Kind DocumentKind `json:"kind"`
	Type DocumentType `json:"type"`
}

type DocumentKind string

const (
	DocumentKindPdf DocumentKind = "pdf"
)

type DocumentType string

const (
	DocumentTypeInvoice DocumentType = "invoice"
)

func NewPdfDocument(id string, data []byte, docType DocumentType) *Document {
	return &Document{
		ID:   id,
		Data: data,
		Kind: DocumentKindPdf,
		Type: docType,
	}
}
