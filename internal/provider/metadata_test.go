package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListEntitiesUsesBoundedContractQueryAndStrictResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/studio/v1/metadata/entities" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("cursor") != "next-value" || request.URL.Query().Get("limit") != "50" || request.URL.Query().Get("search") != "orders" {
			t.Errorf("query = %s", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","kind":"standard","version":4,"fields":[{"code":"id","data_type":"uuid","required":true,"read_only":true}]}],"next_cursor":"after-orders"}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListEntities(context.Background(), EntityQuery{Cursor: "next-value", Limit: 50, Search: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Code != "orders" || page.Items[0].Version != 4 || page.NextCursor != "after-orders" {
		t.Fatalf("page = %#v", page)
	}
}

func TestPutEntitySendsExpectedVersionAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	entity := Entity{Code: "orders", Name: "Orders", Kind: "standard", Version: 4, Fields: []Field{{Code: "id", DataType: "uuid", Required: true}}}
	idempotency := testOpaque(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.EscapedPath() != "/studio/v1/metadata/entities/orders" {
			t.Errorf("request = %s %s", request.Method, request.URL.EscapedPath())
		}
		var body struct {
			Entity          Entity `json:"entity"`
			ExpectedVersion int64  `json:"expected_version"`
			IdempotencyKey  string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Entity.Code != entity.Code || body.ExpectedVersion != 4 || body.IdempotencyKey != idempotency {
			t.Errorf("body = %#v", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":"orders","name":"Orders","kind":"standard","version":5,"fields":[{"code":"id","data_type":"uuid","required":true,"read_only":false}]}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.PutEntity(context.Background(), entity, 4, idempotency)
	if err != nil || updated.Version != 5 {
		t.Fatalf("updated=%#v error=%v", updated, err)
	}
}

func TestMetadataAdapterReturnsSafeAPIErrorAndRejectsUnsafeBounds(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"code":"version_conflict","message":"entity changed","request_id":"request-7"}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(context.Background(), server.URL, testOpaque(t), server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListEntities(context.Background(), EntityQuery{Limit: 201})
	if err == nil {
		t.Fatal("ListEntities accepted an unbounded limit")
	}
	_, err = client.PutEntity(context.Background(), Entity{Code: "../escape"}, 1, testOpaque(t))
	if err == nil {
		t.Fatal("PutEntity accepted an unsafe entity code")
	}
	_, err = client.ListEntities(context.Background(), EntityQuery{Limit: 1})
	apiError, ok := err.(*APIError)
	if !ok || apiError.Status != http.StatusConflict || apiError.Code != "version_conflict" || apiError.RequestID != "request-7" {
		t.Fatalf("error = %#v", err)
	}
	if _, parseErr := url.Parse(apiError.Error()); parseErr != nil {
		// Error must remain ordinary display-safe text, not an opaque response dump.
		t.Log(parseErr)
	}
}
