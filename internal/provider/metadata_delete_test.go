package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataDeletionUsesExactPlannedAndConfirmedBodies(t *testing.T) {
	t.Parallel()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		response.Header().Set("Content-Type", "application/json")
		var body struct {
			Confirmation    string `json:"confirmation"`
			ExpectedVersion int64  `json:"expected_version"`
			IdempotencyKey  string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ExpectedVersion != 7 || body.IdempotencyKey != "opaque-key" {
			t.Fatalf("body=%#v", body)
		}
		switch calls {
		case 1:
			if request.Method != http.MethodPost || request.URL.EscapedPath() != "/studio/v1/metadata/entities/orders/delete-plan" || body.Confirmation != "" {
				t.Fatalf("plan request=%s %s body=%#v", request.Method, request.URL.EscapedPath(), body)
			}
			_, _ = response.Write([]byte(`{"code":"orders","expected_version":7,"deletable":true,"dependencies":[],"impact_summary":"No dependencies."}`))
		case 2:
			if request.Method != http.MethodDelete || request.URL.EscapedPath() != "/studio/v1/metadata/entities/orders" || body.Confirmation != DeleteEntityConfirmation {
				t.Fatalf("delete request=%s %s body=%#v", request.Method, request.URL.EscapedPath(), body)
			}
			_, _ = response.Write([]byte(`{"code":"orders","deleted_version":7,"deleted_at":"2026-07-19T01:02:03Z"}`))
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, "token", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.PlanEntityDeletion(context.Background(), "orders", 7, "opaque-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.DeleteEntity(context.Background(), "orders", 7, "opaque-key"); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataDeletionRejectsProviderMutationBasisDrift(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":"other","expected_version":8,"deletable":true,"dependencies":[],"impact_summary":"unsafe drift"}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, "token", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.PlanEntityDeletion(context.Background(), "orders", 7, "opaque-key"); err == nil {
		t.Fatal("provider mutation-basis drift accepted")
	}
}
