package connection

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Connection struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	ConnectionCode string                 `json:"connection_code"`
	ProviderType   types.SecretProvider   `json:"provider_type"`
	Credentials    map[string]interface{} `json:"credentials"`
	Metadata       map[string]interface{} `json:"metadata"`
	SecretID       string                 `json:"secret_id"`
	types.BaseModel
}

func FromEnt(conn *ent.Connection) *Connection {
	return &Connection{
		ID:             conn.ID,
		Name:           conn.Name,
		Description:    conn.Description,
		ConnectionCode: conn.ConnectionCode,
		ProviderType:   types.SecretProvider(conn.ProviderType),
		Credentials:    conn.Credentials,
		Metadata:       conn.Metadata,
		SecretID:       conn.SecretID,
		BaseModel: types.BaseModel{
			TenantID:  conn.TenantID,
			Status:    types.Status(conn.Status),
			CreatedAt: conn.CreatedAt,
			UpdatedAt: conn.UpdatedAt,
			CreatedBy: conn.CreatedBy,
			UpdatedBy: conn.UpdatedBy,
		},
	}
}

func FromEntList(conns []*ent.Connection) []*Connection {
	result := make([]*Connection, len(conns))
	for i, conn := range conns {
		result[i] = FromEnt(conn)
	}
	return result
}
