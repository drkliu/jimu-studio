package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const (
	// ApplyMigrationConfirmation is the exact Studio 1.0.0 dangerous-operation text.
	ApplyMigrationConfirmation = "APPLY_MIGRATION"
	maxMigrationEntities       = 200
	maxMigrationPlans          = 1000
)

// MigrationPlan is the SQL-free Studio 1.0.0 plan representation.
type MigrationPlan struct {
	EntityCode           string `json:"entity_code"`
	PlanCode             string `json:"plan_code"`
	Risk                 string `json:"risk"`
	Summary              string `json:"summary"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
}

// PlanMigrations previews the unchanged entity versions without applying them.
func (client *Client) PlanMigrations(ctx context.Context, entities []Entity, idempotencyKey string) ([]MigrationPlan, error) {
	return client.migrations(ctx, "/studio/v1/metadata/plan", entities, idempotencyKey, false)
}

// ApplyMigrations applies a previously reviewed entity snapshot with the exact confirmation.
func (client *Client) ApplyMigrations(ctx context.Context, entities []Entity, idempotencyKey string) ([]MigrationPlan, error) {
	return client.migrations(ctx, "/studio/v1/metadata/apply", entities, idempotencyKey, true)
}

func (client *Client) migrations(ctx context.Context, path string, entities []Entity, idempotencyKey string, apply bool) ([]MigrationPlan, error) {
	if len(entities) == 0 || len(entities) > maxMigrationEntities || idempotencyKey == "" || len(idempotencyKey) > 256 || !safeToken.MatchString(idempotencyKey) {
		return nil, errors.New("bounded entities and safe idempotency key are required")
	}
	for _, entity := range entities {
		if err := validateEntity(entity); err != nil {
			return nil, err
		}
	}
	body := struct {
		Confirmation   string   `json:"confirmation,omitempty"`
		Entities       []Entity `json:"entities"`
		IdempotencyKey string   `json:"idempotency_key"`
	}{Entities: entities, IdempotencyKey: idempotencyKey}
	if apply {
		body.Confirmation = ApplyMigrationConfirmation
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(ctx, http.MethodPost, path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	var plans []MigrationPlan
	if err = decodeProviderResponse(response, &plans); err != nil {
		return nil, err
	}
	if len(plans) > maxMigrationPlans {
		return nil, errors.New("provider returned an unbounded migration plan")
	}
	for _, plan := range plans {
		if err = validateMigrationPlan(plan); err != nil {
			return nil, fmt.Errorf("provider returned invalid migration plan: %w", err)
		}
	}
	return plans, nil
}

func validateMigrationPlan(plan MigrationPlan) error {
	if !boundedPlanText(plan.EntityCode, 256) || !boundedPlanText(plan.PlanCode, 256) || !boundedPlanText(plan.Risk, 128) || !boundedPlanText(plan.Summary, 4096) {
		return errors.New("migration plan exceeds safety bounds")
	}
	return nil
}

func boundedPlanText(value string, limit int) bool {
	return value != "" && len(value) <= limit && !hasControl(value)
}
