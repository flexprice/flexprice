// Code generated by ent, DO NOT EDIT.

package feature

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/predicate"
)

// ID filters vertices based on their ID field.
func ID(id string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldID, id))
}

// IDEQ applies the EQ predicate on the ID field.
func IDEQ(id string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldID, id))
}

// IDNEQ applies the NEQ predicate on the ID field.
func IDNEQ(id string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldID, id))
}

// IDIn applies the In predicate on the ID field.
func IDIn(ids ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldID, ids...))
}

// IDNotIn applies the NotIn predicate on the ID field.
func IDNotIn(ids ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldID, ids...))
}

// IDGT applies the GT predicate on the ID field.
func IDGT(id string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldID, id))
}

// IDGTE applies the GTE predicate on the ID field.
func IDGTE(id string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldID, id))
}

// IDLT applies the LT predicate on the ID field.
func IDLT(id string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldID, id))
}

// IDLTE applies the LTE predicate on the ID field.
func IDLTE(id string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldID, id))
}

// IDEqualFold applies the EqualFold predicate on the ID field.
func IDEqualFold(id string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldID, id))
}

// IDContainsFold applies the ContainsFold predicate on the ID field.
func IDContainsFold(id string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldID, id))
}

// TenantID applies equality check predicate on the "tenant_id" field. It's identical to TenantIDEQ.
func TenantID(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldTenantID, v))
}

// Status applies equality check predicate on the "status" field. It's identical to StatusEQ.
func Status(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldStatus, v))
}

// CreatedAt applies equality check predicate on the "created_at" field. It's identical to CreatedAtEQ.
func CreatedAt(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldCreatedAt, v))
}

// UpdatedAt applies equality check predicate on the "updated_at" field. It's identical to UpdatedAtEQ.
func UpdatedAt(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUpdatedAt, v))
}

// CreatedBy applies equality check predicate on the "created_by" field. It's identical to CreatedByEQ.
func CreatedBy(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldCreatedBy, v))
}

// UpdatedBy applies equality check predicate on the "updated_by" field. It's identical to UpdatedByEQ.
func UpdatedBy(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUpdatedBy, v))
}

// LookupKey applies equality check predicate on the "lookup_key" field. It's identical to LookupKeyEQ.
func LookupKey(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldLookupKey, v))
}

// Name applies equality check predicate on the "name" field. It's identical to NameEQ.
func Name(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldName, v))
}

// Description applies equality check predicate on the "description" field. It's identical to DescriptionEQ.
func Description(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldDescription, v))
}

// Type applies equality check predicate on the "type" field. It's identical to TypeEQ.
func Type(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldType, v))
}

// MeterID applies equality check predicate on the "meter_id" field. It's identical to MeterIDEQ.
func MeterID(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldMeterID, v))
}

// UnitSingular applies equality check predicate on the "unit_singular" field. It's identical to UnitSingularEQ.
func UnitSingular(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUnitSingular, v))
}

// UnitPlural applies equality check predicate on the "unit_plural" field. It's identical to UnitPluralEQ.
func UnitPlural(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUnitPlural, v))
}

// TenantIDEQ applies the EQ predicate on the "tenant_id" field.
func TenantIDEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldTenantID, v))
}

// TenantIDNEQ applies the NEQ predicate on the "tenant_id" field.
func TenantIDNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldTenantID, v))
}

// TenantIDIn applies the In predicate on the "tenant_id" field.
func TenantIDIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldTenantID, vs...))
}

// TenantIDNotIn applies the NotIn predicate on the "tenant_id" field.
func TenantIDNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldTenantID, vs...))
}

// TenantIDGT applies the GT predicate on the "tenant_id" field.
func TenantIDGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldTenantID, v))
}

// TenantIDGTE applies the GTE predicate on the "tenant_id" field.
func TenantIDGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldTenantID, v))
}

// TenantIDLT applies the LT predicate on the "tenant_id" field.
func TenantIDLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldTenantID, v))
}

// TenantIDLTE applies the LTE predicate on the "tenant_id" field.
func TenantIDLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldTenantID, v))
}

// TenantIDContains applies the Contains predicate on the "tenant_id" field.
func TenantIDContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldTenantID, v))
}

// TenantIDHasPrefix applies the HasPrefix predicate on the "tenant_id" field.
func TenantIDHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldTenantID, v))
}

// TenantIDHasSuffix applies the HasSuffix predicate on the "tenant_id" field.
func TenantIDHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldTenantID, v))
}

// TenantIDEqualFold applies the EqualFold predicate on the "tenant_id" field.
func TenantIDEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldTenantID, v))
}

// TenantIDContainsFold applies the ContainsFold predicate on the "tenant_id" field.
func TenantIDContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldTenantID, v))
}

// StatusEQ applies the EQ predicate on the "status" field.
func StatusEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldStatus, v))
}

// StatusNEQ applies the NEQ predicate on the "status" field.
func StatusNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldStatus, v))
}

// StatusIn applies the In predicate on the "status" field.
func StatusIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldStatus, vs...))
}

// StatusNotIn applies the NotIn predicate on the "status" field.
func StatusNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldStatus, vs...))
}

// StatusGT applies the GT predicate on the "status" field.
func StatusGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldStatus, v))
}

// StatusGTE applies the GTE predicate on the "status" field.
func StatusGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldStatus, v))
}

// StatusLT applies the LT predicate on the "status" field.
func StatusLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldStatus, v))
}

// StatusLTE applies the LTE predicate on the "status" field.
func StatusLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldStatus, v))
}

// StatusContains applies the Contains predicate on the "status" field.
func StatusContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldStatus, v))
}

// StatusHasPrefix applies the HasPrefix predicate on the "status" field.
func StatusHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldStatus, v))
}

// StatusHasSuffix applies the HasSuffix predicate on the "status" field.
func StatusHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldStatus, v))
}

// StatusEqualFold applies the EqualFold predicate on the "status" field.
func StatusEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldStatus, v))
}

// StatusContainsFold applies the ContainsFold predicate on the "status" field.
func StatusContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldStatus, v))
}

// CreatedAtEQ applies the EQ predicate on the "created_at" field.
func CreatedAtEQ(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldCreatedAt, v))
}

// CreatedAtNEQ applies the NEQ predicate on the "created_at" field.
func CreatedAtNEQ(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldCreatedAt, v))
}

// CreatedAtIn applies the In predicate on the "created_at" field.
func CreatedAtIn(vs ...time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldCreatedAt, vs...))
}

// CreatedAtNotIn applies the NotIn predicate on the "created_at" field.
func CreatedAtNotIn(vs ...time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldCreatedAt, vs...))
}

// CreatedAtGT applies the GT predicate on the "created_at" field.
func CreatedAtGT(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldCreatedAt, v))
}

// CreatedAtGTE applies the GTE predicate on the "created_at" field.
func CreatedAtGTE(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldCreatedAt, v))
}

// CreatedAtLT applies the LT predicate on the "created_at" field.
func CreatedAtLT(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldCreatedAt, v))
}

// CreatedAtLTE applies the LTE predicate on the "created_at" field.
func CreatedAtLTE(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldCreatedAt, v))
}

// UpdatedAtEQ applies the EQ predicate on the "updated_at" field.
func UpdatedAtEQ(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUpdatedAt, v))
}

// UpdatedAtNEQ applies the NEQ predicate on the "updated_at" field.
func UpdatedAtNEQ(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldUpdatedAt, v))
}

// UpdatedAtIn applies the In predicate on the "updated_at" field.
func UpdatedAtIn(vs ...time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldUpdatedAt, vs...))
}

// UpdatedAtNotIn applies the NotIn predicate on the "updated_at" field.
func UpdatedAtNotIn(vs ...time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldUpdatedAt, vs...))
}

// UpdatedAtGT applies the GT predicate on the "updated_at" field.
func UpdatedAtGT(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldUpdatedAt, v))
}

// UpdatedAtGTE applies the GTE predicate on the "updated_at" field.
func UpdatedAtGTE(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldUpdatedAt, v))
}

// UpdatedAtLT applies the LT predicate on the "updated_at" field.
func UpdatedAtLT(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldUpdatedAt, v))
}

// UpdatedAtLTE applies the LTE predicate on the "updated_at" field.
func UpdatedAtLTE(v time.Time) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldUpdatedAt, v))
}

// CreatedByEQ applies the EQ predicate on the "created_by" field.
func CreatedByEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldCreatedBy, v))
}

// CreatedByNEQ applies the NEQ predicate on the "created_by" field.
func CreatedByNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldCreatedBy, v))
}

// CreatedByIn applies the In predicate on the "created_by" field.
func CreatedByIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldCreatedBy, vs...))
}

// CreatedByNotIn applies the NotIn predicate on the "created_by" field.
func CreatedByNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldCreatedBy, vs...))
}

// CreatedByGT applies the GT predicate on the "created_by" field.
func CreatedByGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldCreatedBy, v))
}

// CreatedByGTE applies the GTE predicate on the "created_by" field.
func CreatedByGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldCreatedBy, v))
}

// CreatedByLT applies the LT predicate on the "created_by" field.
func CreatedByLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldCreatedBy, v))
}

// CreatedByLTE applies the LTE predicate on the "created_by" field.
func CreatedByLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldCreatedBy, v))
}

// CreatedByContains applies the Contains predicate on the "created_by" field.
func CreatedByContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldCreatedBy, v))
}

// CreatedByHasPrefix applies the HasPrefix predicate on the "created_by" field.
func CreatedByHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldCreatedBy, v))
}

// CreatedByHasSuffix applies the HasSuffix predicate on the "created_by" field.
func CreatedByHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldCreatedBy, v))
}

// CreatedByIsNil applies the IsNil predicate on the "created_by" field.
func CreatedByIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldCreatedBy))
}

// CreatedByNotNil applies the NotNil predicate on the "created_by" field.
func CreatedByNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldCreatedBy))
}

// CreatedByEqualFold applies the EqualFold predicate on the "created_by" field.
func CreatedByEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldCreatedBy, v))
}

// CreatedByContainsFold applies the ContainsFold predicate on the "created_by" field.
func CreatedByContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldCreatedBy, v))
}

// UpdatedByEQ applies the EQ predicate on the "updated_by" field.
func UpdatedByEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUpdatedBy, v))
}

// UpdatedByNEQ applies the NEQ predicate on the "updated_by" field.
func UpdatedByNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldUpdatedBy, v))
}

// UpdatedByIn applies the In predicate on the "updated_by" field.
func UpdatedByIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldUpdatedBy, vs...))
}

// UpdatedByNotIn applies the NotIn predicate on the "updated_by" field.
func UpdatedByNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldUpdatedBy, vs...))
}

// UpdatedByGT applies the GT predicate on the "updated_by" field.
func UpdatedByGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldUpdatedBy, v))
}

// UpdatedByGTE applies the GTE predicate on the "updated_by" field.
func UpdatedByGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldUpdatedBy, v))
}

// UpdatedByLT applies the LT predicate on the "updated_by" field.
func UpdatedByLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldUpdatedBy, v))
}

// UpdatedByLTE applies the LTE predicate on the "updated_by" field.
func UpdatedByLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldUpdatedBy, v))
}

// UpdatedByContains applies the Contains predicate on the "updated_by" field.
func UpdatedByContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldUpdatedBy, v))
}

// UpdatedByHasPrefix applies the HasPrefix predicate on the "updated_by" field.
func UpdatedByHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldUpdatedBy, v))
}

// UpdatedByHasSuffix applies the HasSuffix predicate on the "updated_by" field.
func UpdatedByHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldUpdatedBy, v))
}

// UpdatedByIsNil applies the IsNil predicate on the "updated_by" field.
func UpdatedByIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldUpdatedBy))
}

// UpdatedByNotNil applies the NotNil predicate on the "updated_by" field.
func UpdatedByNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldUpdatedBy))
}

// UpdatedByEqualFold applies the EqualFold predicate on the "updated_by" field.
func UpdatedByEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldUpdatedBy, v))
}

// UpdatedByContainsFold applies the ContainsFold predicate on the "updated_by" field.
func UpdatedByContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldUpdatedBy, v))
}

// LookupKeyEQ applies the EQ predicate on the "lookup_key" field.
func LookupKeyEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldLookupKey, v))
}

// LookupKeyNEQ applies the NEQ predicate on the "lookup_key" field.
func LookupKeyNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldLookupKey, v))
}

// LookupKeyIn applies the In predicate on the "lookup_key" field.
func LookupKeyIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldLookupKey, vs...))
}

// LookupKeyNotIn applies the NotIn predicate on the "lookup_key" field.
func LookupKeyNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldLookupKey, vs...))
}

// LookupKeyGT applies the GT predicate on the "lookup_key" field.
func LookupKeyGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldLookupKey, v))
}

// LookupKeyGTE applies the GTE predicate on the "lookup_key" field.
func LookupKeyGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldLookupKey, v))
}

// LookupKeyLT applies the LT predicate on the "lookup_key" field.
func LookupKeyLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldLookupKey, v))
}

// LookupKeyLTE applies the LTE predicate on the "lookup_key" field.
func LookupKeyLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldLookupKey, v))
}

// LookupKeyContains applies the Contains predicate on the "lookup_key" field.
func LookupKeyContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldLookupKey, v))
}

// LookupKeyHasPrefix applies the HasPrefix predicate on the "lookup_key" field.
func LookupKeyHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldLookupKey, v))
}

// LookupKeyHasSuffix applies the HasSuffix predicate on the "lookup_key" field.
func LookupKeyHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldLookupKey, v))
}

// LookupKeyEqualFold applies the EqualFold predicate on the "lookup_key" field.
func LookupKeyEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldLookupKey, v))
}

// LookupKeyContainsFold applies the ContainsFold predicate on the "lookup_key" field.
func LookupKeyContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldLookupKey, v))
}

// NameEQ applies the EQ predicate on the "name" field.
func NameEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldName, v))
}

// NameNEQ applies the NEQ predicate on the "name" field.
func NameNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldName, v))
}

// NameIn applies the In predicate on the "name" field.
func NameIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldName, vs...))
}

// NameNotIn applies the NotIn predicate on the "name" field.
func NameNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldName, vs...))
}

// NameGT applies the GT predicate on the "name" field.
func NameGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldName, v))
}

// NameGTE applies the GTE predicate on the "name" field.
func NameGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldName, v))
}

// NameLT applies the LT predicate on the "name" field.
func NameLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldName, v))
}

// NameLTE applies the LTE predicate on the "name" field.
func NameLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldName, v))
}

// NameContains applies the Contains predicate on the "name" field.
func NameContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldName, v))
}

// NameHasPrefix applies the HasPrefix predicate on the "name" field.
func NameHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldName, v))
}

// NameHasSuffix applies the HasSuffix predicate on the "name" field.
func NameHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldName, v))
}

// NameEqualFold applies the EqualFold predicate on the "name" field.
func NameEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldName, v))
}

// NameContainsFold applies the ContainsFold predicate on the "name" field.
func NameContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldName, v))
}

// DescriptionEQ applies the EQ predicate on the "description" field.
func DescriptionEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldDescription, v))
}

// DescriptionNEQ applies the NEQ predicate on the "description" field.
func DescriptionNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldDescription, v))
}

// DescriptionIn applies the In predicate on the "description" field.
func DescriptionIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldDescription, vs...))
}

// DescriptionNotIn applies the NotIn predicate on the "description" field.
func DescriptionNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldDescription, vs...))
}

// DescriptionGT applies the GT predicate on the "description" field.
func DescriptionGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldDescription, v))
}

// DescriptionGTE applies the GTE predicate on the "description" field.
func DescriptionGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldDescription, v))
}

// DescriptionLT applies the LT predicate on the "description" field.
func DescriptionLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldDescription, v))
}

// DescriptionLTE applies the LTE predicate on the "description" field.
func DescriptionLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldDescription, v))
}

// DescriptionContains applies the Contains predicate on the "description" field.
func DescriptionContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldDescription, v))
}

// DescriptionHasPrefix applies the HasPrefix predicate on the "description" field.
func DescriptionHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldDescription, v))
}

// DescriptionHasSuffix applies the HasSuffix predicate on the "description" field.
func DescriptionHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldDescription, v))
}

// DescriptionIsNil applies the IsNil predicate on the "description" field.
func DescriptionIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldDescription))
}

// DescriptionNotNil applies the NotNil predicate on the "description" field.
func DescriptionNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldDescription))
}

// DescriptionEqualFold applies the EqualFold predicate on the "description" field.
func DescriptionEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldDescription, v))
}

// DescriptionContainsFold applies the ContainsFold predicate on the "description" field.
func DescriptionContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldDescription, v))
}

// TypeEQ applies the EQ predicate on the "type" field.
func TypeEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldType, v))
}

// TypeNEQ applies the NEQ predicate on the "type" field.
func TypeNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldType, v))
}

// TypeIn applies the In predicate on the "type" field.
func TypeIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldType, vs...))
}

// TypeNotIn applies the NotIn predicate on the "type" field.
func TypeNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldType, vs...))
}

// TypeGT applies the GT predicate on the "type" field.
func TypeGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldType, v))
}

// TypeGTE applies the GTE predicate on the "type" field.
func TypeGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldType, v))
}

// TypeLT applies the LT predicate on the "type" field.
func TypeLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldType, v))
}

// TypeLTE applies the LTE predicate on the "type" field.
func TypeLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldType, v))
}

// TypeContains applies the Contains predicate on the "type" field.
func TypeContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldType, v))
}

// TypeHasPrefix applies the HasPrefix predicate on the "type" field.
func TypeHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldType, v))
}

// TypeHasSuffix applies the HasSuffix predicate on the "type" field.
func TypeHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldType, v))
}

// TypeEqualFold applies the EqualFold predicate on the "type" field.
func TypeEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldType, v))
}

// TypeContainsFold applies the ContainsFold predicate on the "type" field.
func TypeContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldType, v))
}

// MeterIDEQ applies the EQ predicate on the "meter_id" field.
func MeterIDEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldMeterID, v))
}

// MeterIDNEQ applies the NEQ predicate on the "meter_id" field.
func MeterIDNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldMeterID, v))
}

// MeterIDIn applies the In predicate on the "meter_id" field.
func MeterIDIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldMeterID, vs...))
}

// MeterIDNotIn applies the NotIn predicate on the "meter_id" field.
func MeterIDNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldMeterID, vs...))
}

// MeterIDGT applies the GT predicate on the "meter_id" field.
func MeterIDGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldMeterID, v))
}

// MeterIDGTE applies the GTE predicate on the "meter_id" field.
func MeterIDGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldMeterID, v))
}

// MeterIDLT applies the LT predicate on the "meter_id" field.
func MeterIDLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldMeterID, v))
}

// MeterIDLTE applies the LTE predicate on the "meter_id" field.
func MeterIDLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldMeterID, v))
}

// MeterIDContains applies the Contains predicate on the "meter_id" field.
func MeterIDContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldMeterID, v))
}

// MeterIDHasPrefix applies the HasPrefix predicate on the "meter_id" field.
func MeterIDHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldMeterID, v))
}

// MeterIDHasSuffix applies the HasSuffix predicate on the "meter_id" field.
func MeterIDHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldMeterID, v))
}

// MeterIDIsNil applies the IsNil predicate on the "meter_id" field.
func MeterIDIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldMeterID))
}

// MeterIDNotNil applies the NotNil predicate on the "meter_id" field.
func MeterIDNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldMeterID))
}

// MeterIDEqualFold applies the EqualFold predicate on the "meter_id" field.
func MeterIDEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldMeterID, v))
}

// MeterIDContainsFold applies the ContainsFold predicate on the "meter_id" field.
func MeterIDContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldMeterID, v))
}

// MetadataIsNil applies the IsNil predicate on the "metadata" field.
func MetadataIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldMetadata))
}

// MetadataNotNil applies the NotNil predicate on the "metadata" field.
func MetadataNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldMetadata))
}

// UnitSingularEQ applies the EQ predicate on the "unit_singular" field.
func UnitSingularEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUnitSingular, v))
}

// UnitSingularNEQ applies the NEQ predicate on the "unit_singular" field.
func UnitSingularNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldUnitSingular, v))
}

// UnitSingularIn applies the In predicate on the "unit_singular" field.
func UnitSingularIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldUnitSingular, vs...))
}

// UnitSingularNotIn applies the NotIn predicate on the "unit_singular" field.
func UnitSingularNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldUnitSingular, vs...))
}

// UnitSingularGT applies the GT predicate on the "unit_singular" field.
func UnitSingularGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldUnitSingular, v))
}

// UnitSingularGTE applies the GTE predicate on the "unit_singular" field.
func UnitSingularGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldUnitSingular, v))
}

// UnitSingularLT applies the LT predicate on the "unit_singular" field.
func UnitSingularLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldUnitSingular, v))
}

// UnitSingularLTE applies the LTE predicate on the "unit_singular" field.
func UnitSingularLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldUnitSingular, v))
}

// UnitSingularContains applies the Contains predicate on the "unit_singular" field.
func UnitSingularContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldUnitSingular, v))
}

// UnitSingularHasPrefix applies the HasPrefix predicate on the "unit_singular" field.
func UnitSingularHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldUnitSingular, v))
}

// UnitSingularHasSuffix applies the HasSuffix predicate on the "unit_singular" field.
func UnitSingularHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldUnitSingular, v))
}

// UnitSingularIsNil applies the IsNil predicate on the "unit_singular" field.
func UnitSingularIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldUnitSingular))
}

// UnitSingularNotNil applies the NotNil predicate on the "unit_singular" field.
func UnitSingularNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldUnitSingular))
}

// UnitSingularEqualFold applies the EqualFold predicate on the "unit_singular" field.
func UnitSingularEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldUnitSingular, v))
}

// UnitSingularContainsFold applies the ContainsFold predicate on the "unit_singular" field.
func UnitSingularContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldUnitSingular, v))
}

// UnitPluralEQ applies the EQ predicate on the "unit_plural" field.
func UnitPluralEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEQ(FieldUnitPlural, v))
}

// UnitPluralNEQ applies the NEQ predicate on the "unit_plural" field.
func UnitPluralNEQ(v string) predicate.Feature {
	return predicate.Feature(sql.FieldNEQ(FieldUnitPlural, v))
}

// UnitPluralIn applies the In predicate on the "unit_plural" field.
func UnitPluralIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldIn(FieldUnitPlural, vs...))
}

// UnitPluralNotIn applies the NotIn predicate on the "unit_plural" field.
func UnitPluralNotIn(vs ...string) predicate.Feature {
	return predicate.Feature(sql.FieldNotIn(FieldUnitPlural, vs...))
}

// UnitPluralGT applies the GT predicate on the "unit_plural" field.
func UnitPluralGT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGT(FieldUnitPlural, v))
}

// UnitPluralGTE applies the GTE predicate on the "unit_plural" field.
func UnitPluralGTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldGTE(FieldUnitPlural, v))
}

// UnitPluralLT applies the LT predicate on the "unit_plural" field.
func UnitPluralLT(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLT(FieldUnitPlural, v))
}

// UnitPluralLTE applies the LTE predicate on the "unit_plural" field.
func UnitPluralLTE(v string) predicate.Feature {
	return predicate.Feature(sql.FieldLTE(FieldUnitPlural, v))
}

// UnitPluralContains applies the Contains predicate on the "unit_plural" field.
func UnitPluralContains(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContains(FieldUnitPlural, v))
}

// UnitPluralHasPrefix applies the HasPrefix predicate on the "unit_plural" field.
func UnitPluralHasPrefix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasPrefix(FieldUnitPlural, v))
}

// UnitPluralHasSuffix applies the HasSuffix predicate on the "unit_plural" field.
func UnitPluralHasSuffix(v string) predicate.Feature {
	return predicate.Feature(sql.FieldHasSuffix(FieldUnitPlural, v))
}

// UnitPluralIsNil applies the IsNil predicate on the "unit_plural" field.
func UnitPluralIsNil() predicate.Feature {
	return predicate.Feature(sql.FieldIsNull(FieldUnitPlural))
}

// UnitPluralNotNil applies the NotNil predicate on the "unit_plural" field.
func UnitPluralNotNil() predicate.Feature {
	return predicate.Feature(sql.FieldNotNull(FieldUnitPlural))
}

// UnitPluralEqualFold applies the EqualFold predicate on the "unit_plural" field.
func UnitPluralEqualFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldEqualFold(FieldUnitPlural, v))
}

// UnitPluralContainsFold applies the ContainsFold predicate on the "unit_plural" field.
func UnitPluralContainsFold(v string) predicate.Feature {
	return predicate.Feature(sql.FieldContainsFold(FieldUnitPlural, v))
}

// And groups predicates with the AND operator between them.
func And(predicates ...predicate.Feature) predicate.Feature {
	return predicate.Feature(sql.AndPredicates(predicates...))
}

// Or groups predicates with the OR operator between them.
func Or(predicates ...predicate.Feature) predicate.Feature {
	return predicate.Feature(sql.OrPredicates(predicates...))
}

// Not applies the not operator on the given predicate.
func Not(p predicate.Feature) predicate.Feature {
	return predicate.Feature(sql.NotPredicates(p))
}
