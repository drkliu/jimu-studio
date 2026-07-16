package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityListsAreBoundedAndMutationsUseExactContract(t *testing.T) {
	t.Parallel()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		seen = append(seen, request.Method+" "+request.URL.RequestURI())
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			if request.URL.Query().Get("limit") != "50" || request.URL.Query().Get("cursor") != "next" || request.URL.Query().Get("search") != "needle" {
				t.Errorf("query=%s", request.URL.RawQuery)
			}
			if request.URL.Path == "/studio/v1/identity/users" {
				_, _ = response.Write([]byte(`{"items":[{"id":"user-1","display_name":"User One","email":"one@example.test","status":"active","roles":["admin"],"version":3}],"next_cursor":"users-2"}`))
			} else {
				_, _ = response.Write([]byte(`{"items":[{"key":"admin","display_name":"Administrator","system":true,"version":2}],"next_cursor":"roles-2"}`))
			}
			return
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch request.URL.Path {
		case "/studio/v1/identity/users":
			if len(body) != 4 || body["idempotency_key"] != "create-user-key" {
				t.Errorf("create user body=%#v", body)
			}
		case "/studio/v1/identity/users/user-1/status":
			if body["confirmation"] != DisableUserConfirmation || body["expected_version"] != float64(3) {
				t.Errorf("status body=%#v", body)
			}
		case "/studio/v1/identity/roles":
			if len(body) != 3 || body["idempotency_key"] != "create-role-key" {
				t.Errorf("create role body=%#v", body)
			}
		case "/studio/v1/identity/roles/operator":
			if body["expected_version"] != float64(4) {
				t.Errorf("update role body=%#v", body)
			}
		case "/studio/v1/identity/users/user-1/roles/operator":
			if body["expected_version"] != float64(3) || body["idempotency_key"] == "" {
				t.Errorf("change role body=%#v", body)
			}
		}
		if request.URL.Path == "/studio/v1/identity/roles" || request.URL.Path == "/studio/v1/identity/roles/operator" {
			_, _ = response.Write([]byte(`{"key":"operator","display_name":"Operator","system":false,"version":4}`))
		} else {
			_, _ = response.Write([]byte(`{"id":"user-1","display_name":"User One","email":"one@example.test","status":"active","roles":["admin"],"version":3}`))
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	query := IdentityQuery{Cursor: "next", Limit: 50, Search: "needle"}
	if page, err := client.ListUsers(context.Background(), query); err != nil || len(page.Items) != 1 || page.NextCursor != "users-2" {
		t.Fatalf("users=%#v err=%v", page, err)
	}
	if page, err := client.ListRoles(context.Background(), query); err != nil || len(page.Items) != 1 || !page.Items[0].System {
		t.Fatalf("roles=%#v err=%v", page, err)
	}
	if _, err = client.CreateUser(context.Background(), User{ID: "user-1", DisplayName: "User One", Email: "one@example.test"}, "create-user-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.SetUserStatus(context.Background(), "user-1", "disabled", 3, "status-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.CreateRole(context.Background(), Role{Key: "operator", DisplayName: "Operator"}, "create-role-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.UpdateRole(context.Background(), "operator", "Operator", 4, "update-role-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ChangeUserRole(context.Background(), "user-1", "operator", 3, "assign-key", true); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ChangeUserRole(context.Background(), "user-1", "operator", 3, "revoke-key", false); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 8 || seen[6] != "PUT /studio/v1/identity/users/user-1/roles/operator" || seen[7] != "DELETE /studio/v1/identity/users/user-1/roles/operator" {
		t.Fatalf("requests=%v", seen)
	}
}

func TestIdentityAdapterRejectsUnsafeStatusAndBounds(t *testing.T) {
	client, err := NewClient(context.Background(), "https://provider.example", testOpaque(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListUsers(context.Background(), IdentityQuery{Limit: 201}); err == nil {
		t.Fatal("unbounded users accepted")
	}
	if _, err = client.SetUserStatus(context.Background(), "user", "deleted", 1, "key"); err == nil {
		t.Fatal("unsafe status accepted")
	}
	if _, err = client.ChangeUserRole(context.Background(), "../user", "role", 1, "key", true); err == nil {
		t.Fatal("unsafe user path accepted")
	}
}
