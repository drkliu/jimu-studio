package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowOperationsUseDiscoverableVersionedTaskContract(t *testing.T) {
	t.Parallel()
	key := testOpaque(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/workflows/runs":
			if request.URL.Query().Get("limit") != "50" || request.URL.Query().Get("search") != "checkout" {
				t.Errorf("run query = %s", request.URL.RawQuery)
			}
			_, _ = response.Write([]byte(`{"items":[{"id":"run-1","workflow":"checkout","state":"running","version":7,"created_at":"2026-07-16T10:00:00Z","updated_at":"2026-07-16T10:01:00Z","lease_state":"active","lease_expires_at":"2026-07-16T10:02:00Z"}]}`))
		case "/studio/v1/workflows/runs/run-1/tasks":
			_, _ = response.Write([]byte(`{"items":[{"id":"task-1","run_id":"run-1","code":"charge","state":"retry_waiting","attempt":2,"version":9,"next_attempt_at":"2026-07-16T10:03:00Z","error_code":"provider_timeout","lease_state":"expired","recovery_state":"lease_expired"}]}`))
		case "/studio/v1/workflows/tasks/task-1/retry":
			var body struct {
				Confirmation    string `json:"confirmation"`
				ExpectedVersion int64  `json:"expected_version"`
				IdempotencyKey  string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if request.Method != http.MethodPost || body.Confirmation != RetryTaskConfirmation || body.ExpectedVersion != 9 || body.IdempotencyKey != key {
				t.Errorf("retry request = %s %#v", request.Method, body)
			}
			_, _ = response.Write([]byte(`{"id":"run-1","workflow":"checkout","state":"running","version":8,"created_at":"2026-07-16T10:00:00Z","updated_at":"2026-07-16T10:04:00Z","lease_state":"none"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := client.ListWorkflowRuns(context.Background(), OperationsQuery{Limit: 50, Search: "checkout"})
	if err != nil || len(runs.Items) != 1 || runs.Items[0].LeaseState != "active" {
		t.Fatalf("runs=%#v error=%v", runs, err)
	}
	tasks, err := client.ListWorkflowTasks(context.Background(), "run-1", OperationsQuery{Limit: 50})
	if err != nil || len(tasks.Items) != 1 || tasks.Items[0].Version != 9 || tasks.Items[0].RecoveryState != "lease_expired" {
		t.Fatalf("tasks=%#v error=%v", tasks, err)
	}
	if _, err = client.RetryWorkflowTask(context.Background(), "task-1", 9, key); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaAndAuditOperationsAreBoundedAndExact(t *testing.T) {
	t.Parallel()
	key := testOpaque(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/quota/plans":
			if request.Method == http.MethodPost {
				var body struct {
					Confirmation    string    `json:"confirmation"`
					ExpectedVersion int64     `json:"expected_version"`
					IdempotencyKey  string    `json:"idempotency_key"`
					Plan            QuotaPlan `json:"plan"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Confirmation != PublishPlanConfirmation || body.ExpectedVersion != 3 || body.IdempotencyKey != key || body.Plan.Code != "standard" {
					t.Errorf("publish body = %#v", body)
				}
				_, _ = response.Write([]byte(`{"code":"standard","version":4,"effective_at":"2026-08-01T00:00:00Z","window_seconds":60,"limits":{"requests":100}}`))
				return
			}
			_, _ = response.Write([]byte(`{"items":[]}`))
		case "/studio/v1/audit":
			_, _ = response.Write([]byte(`{"items":[{"id":"audit-1","actor_user_id":"operator","action":"quota.publish","target_type":"quota_plan","target_id":"standard","occurred_at":"2026-07-16T10:00:00Z","details":{},"redacted_paths":["details.secret"]}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	plan := QuotaPlan{Code: "standard", Version: 3, EffectiveAt: "2026-08-01T00:00:00Z", WindowSeconds: 60, Limits: map[string]any{"requests": 100}}
	if _, err = client.PublishQuotaPlan(context.Background(), plan, 3, key); err != nil {
		t.Fatal(err)
	}
	audit, err := client.ListAudit(context.Background(), OperationsQuery{Limit: 50})
	if err != nil || len(audit.Items) != 1 || len(audit.Items[0].RedactedPaths) != 1 {
		t.Fatalf("audit=%#v error=%v", audit, err)
	}
	if _, err = client.ListQuotaPlans(context.Background(), OperationsQuery{Limit: 201}); err == nil {
		t.Fatal("unbounded quota query was accepted")
	}
}
