package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const DeleteEntityConfirmation = "DELETE_ENTITY"

// DeletionPlan is the provider-authoritative dependency and impact preview.
type DeletionPlan struct {
	Code            string   `json:"code"`
	ExpectedVersion int64    `json:"expected_version"`
	Deletable       bool     `json:"deletable"`
	Dependencies    []string `json:"dependencies"`
	ImpactSummary   string   `json:"impact_summary"`
}

// DeletionReceipt is the auditable result of a completed entity deletion.
type DeletionReceipt struct {
	Code           string `json:"code"`
	DeletedVersion int64  `json:"deleted_version"`
	DeletedAt      string `json:"deleted_at"`
}

// PlanEntityDeletion previews dependencies without deleting the entity.
func (client *Client) PlanEntityDeletion(ctx context.Context, code string, expectedVersion int64, idempotencyKey string) (DeletionPlan, error) {
	body, err := metadataDeletionBody(code, expectedVersion, idempotencyKey, false)
	if err != nil {
		return DeletionPlan{}, err
	}
	response, err := client.Do(ctx, http.MethodPost, "/studio/v1/metadata/entities/"+url.PathEscape(code)+"/delete-plan", bytes.NewReader(body))
	if err != nil {
		return DeletionPlan{}, err
	}
	var plan DeletionPlan
	if err = decodeProviderResponse(response, &plan); err != nil {
		return DeletionPlan{}, err
	}
	if err = validateDeletionPlan(plan, code, expectedVersion); err != nil {
		return DeletionPlan{}, fmt.Errorf("provider returned invalid deletion plan: %w", err)
	}
	return plan, nil
}

// DeleteEntity performs a confirmed, optimistic, idempotent deletion.
func (client *Client) DeleteEntity(ctx context.Context, code string, expectedVersion int64, idempotencyKey string) (DeletionReceipt, error) {
	body, err := metadataDeletionBody(code, expectedVersion, idempotencyKey, true)
	if err != nil {
		return DeletionReceipt{}, err
	}
	response, err := client.Do(ctx, http.MethodDelete, "/studio/v1/metadata/entities/"+url.PathEscape(code), bytes.NewReader(body))
	if err != nil {
		return DeletionReceipt{}, err
	}
	var receipt DeletionReceipt
	if err = decodeProviderResponse(response, &receipt); err != nil {
		return DeletionReceipt{}, err
	}
	if receipt.Code != code || receipt.DeletedVersion != expectedVersion {
		return DeletionReceipt{}, errors.New("provider returned a deletion receipt for a different entity version")
	}
	if _, err = time.Parse(time.RFC3339, receipt.DeletedAt); err != nil {
		return DeletionReceipt{}, errors.New("provider returned an invalid deletion timestamp")
	}
	return receipt, nil
}

func metadataDeletionBody(code string, expectedVersion int64, idempotencyKey string, confirmed bool) ([]byte, error) {
	if !safePathValue(code, 256) || expectedVersion < 0 || idempotencyKey == "" || len(idempotencyKey) > 256 || !safeToken.MatchString(idempotencyKey) {
		return nil, errors.New("entity code, expected version, and safe idempotency key are required")
	}
	body := struct {
		Confirmation    string `json:"confirmation,omitempty"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{ExpectedVersion: expectedVersion, IdempotencyKey: idempotencyKey}
	if confirmed {
		body.Confirmation = DeleteEntityConfirmation
	}
	return json.Marshal(body)
}

func validateDeletionPlan(plan DeletionPlan, code string, expectedVersion int64) error {
	if plan.Code != code || plan.ExpectedVersion != expectedVersion || len(plan.Dependencies) > 200 || len(plan.ImpactSummary) > 4096 || hasControl(plan.ImpactSummary) {
		return errors.New("deletion plan exceeds safety bounds or changed its mutation basis")
	}
	for _, dependency := range plan.Dependencies {
		if dependency == "" || len(dependency) > 512 || hasControl(dependency) {
			return errors.New("deletion dependency exceeds safety bounds")
		}
	}
	return nil
}
