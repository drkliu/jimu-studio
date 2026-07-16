package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMigrationPlanAndApplyUseExactContractBodies(t *testing.T) {
	t.Parallel()
	entity := Entity{Code: "orders", Name: "Orders", Version: 7, Fields: []Field{{Code: "id", DataType: "uuid"}}}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/metadata/plan":
			if request.Method != http.MethodPost || len(body) != 2 || body["idempotency_key"] != "plan-key" || body["confirmation"] != nil {
				t.Errorf("plan request method=%s body=%#v", request.Method, body)
			}
		case "/studio/v1/metadata/apply":
			if request.Method != http.MethodPost || len(body) != 3 || body["idempotency_key"] != "apply-key" || body["confirmation"] != ApplyMigrationConfirmation {
				t.Errorf("apply request method=%s body=%#v", request.Method, body)
			}
		default:
			t.Errorf("path=%s", request.URL.Path)
		}
		entities, ok := body["entities"].([]any)
		if !ok || len(entities) != 1 || entities[0].(map[string]any)["version"] != float64(7) {
			t.Errorf("entities=%#v", body["entities"])
		}
		_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"add_orders","risk":"low","summary":"Create the orders table","requires_confirmation":true}]`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := client.PlanMigrations(context.Background(), []Entity{entity}, "plan-key")
	if err != nil || len(plans) != 1 || plans[0].PlanCode != "add_orders" {
		t.Fatalf("PlanMigrations() plans=%#v error=%v", plans, err)
	}
	applied, err := client.ApplyMigrations(context.Background(), []Entity{entity}, "apply-key")
	if err != nil || len(applied) != 1 || applied[0].Risk != "low" {
		t.Fatalf("ApplyMigrations() plans=%#v error=%v", applied, err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestMigrationResponsesRejectSQLAndUnsafeBounds(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"unsafe","risk":"high","summary":"change","requires_confirmation":true,"sql":"DROP TABLE orders"}]`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	entity := Entity{Code: "orders", Name: "Orders", Version: 7, Fields: []Field{}}
	if _, err = client.PlanMigrations(context.Background(), []Entity{entity}, "plan-key"); err == nil {
		t.Fatal("plan response accepted a raw SQL field")
	}
	if _, err = client.PlanMigrations(context.Background(), make([]Entity, 201), "plan-key"); err == nil {
		t.Fatal("plan request accepted an unbounded entity set")
	}
}
