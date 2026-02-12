package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/oklog/ulid/v2"
)

// WorkflowExecution holds the schema definition for the WorkflowExecution entity.
type WorkflowExecution struct {
	ent.Schema
}

// Mixin of the WorkflowExecution.
func (WorkflowExecution) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the WorkflowExecution.
func (WorkflowExecution) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(func() string {
				return ulid.Make().String()
			}).
			SchemaType(map[string]string{
				"postgres": "varchar(26)",
			}).
			Unique().
			Immutable().
			Comment("ULID primary key"),
		field.String("workflow_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Comment("User-provided workflow ID"),
		field.String("run_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Comment("Temporal-generated UUID for this specific execution"),
		field.String("workflow_type").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			NotEmpty().
			Comment("Workflow type name (e.g., BillingWorkflow, PriceSyncWorkflow)"),
		field.String("task_queue").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			NotEmpty().
			Comment("Temporal task queue name"),
		field.Time("start_time").
			Comment("Workflow execution start time"),
		field.Time("end_time").
			Optional().
			Nillable().
			Comment("Workflow execution end time (when workflow completed/failed)"),
		field.Int64("duration_ms").
			Optional().
			Nillable().
			Comment("Total workflow execution duration in milliseconds"),
		field.String("workflow_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.WorkflowExecutionStatusUnknown)).
			GoType(types.WorkflowExecutionStatus("")).
			Comment("Temporal workflow run status (Running, Completed, Failed, etc.)"),
		field.String("entity").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Comment("Entity type (e.g. plan, invoice, subscription) for efficient filtering"),
		field.String("entity_id").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Comment("Entity ID (e.g. plan ID, invoice ID) for efficient filtering"),
		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Comment("Custom metadata (e.g., customer_id, plan, etc.)"),
	}
}

// Edges of the WorkflowExecution.
func (WorkflowExecution) Edges() []ent.Edge {
	return nil // No edges for now
}

// Indexes of the WorkflowExecution.
func (WorkflowExecution) Indexes() []ent.Index {
	return []ent.Index{
		// Unique constraint on workflow_id + run_id combination
		index.Fields("workflow_id", "run_id").
			Unique().
			StorageKey("idx_workflow_executions_workflow_run_unique"),
		// Composite index for filtering by tenant, environment, and workflow type
		index.Fields("tenant_id", "environment_id", "workflow_type").
			StorageKey("idx_workflow_executions_tenant_env_type"),
		// Composite index for filtering by tenant, environment, and task queue
		index.Fields("tenant_id", "environment_id", "task_queue").
			StorageKey("idx_workflow_executions_tenant_env_queue"),
		// Index on start_time for time-based filtering
		index.Fields("start_time").
			StorageKey("idx_workflow_executions_start_time"),
		// Composite index for tenant and environment filtering
		index.Fields("tenant_id", "environment_id", "start_time").
			StorageKey("idx_workflow_executions_tenant_env_time"),
		// Index for filtering by workflow status (e.g. Running, Failed)
		index.Fields("tenant_id", "environment_id", "workflow_status").
			StorageKey("idx_workflow_executions_tenant_env_status"),
		// Index for filtering by entity and entity_id (replaces metadata JSONB filter)
		index.Fields("tenant_id", "environment_id", "entity", "entity_id").
			StorageKey("idx_workflow_executions_tenant_env_entity"),
	}
}
