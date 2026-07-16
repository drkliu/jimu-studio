package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxMetadataResponse = 2 << 20
	maxErrorResponse    = 64 << 10
)

// Field is the exact Studio 1.0.0 metadata field representation.
type Field struct {
	Code     string `json:"code"`
	DataType string `json:"data_type"`
	Required bool   `json:"required,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// Entity is the exact Studio 1.0.0 metadata entity representation.
type Entity struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Kind    string  `json:"kind,omitempty"`
	Version int64   `json:"version"`
	Fields  []Field `json:"fields"`
}

// EntityPage is one bounded cursor page.
type EntityPage struct {
	Items      []Entity `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// EntityQuery maps the contract's bounded list parameters.
type EntityQuery struct {
	Cursor string
	Limit  int
	Search string
}

// APIError is a bounded, display-safe provider error.
type APIError struct {
	Status    int
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func (apiError *APIError) Error() string {
	if apiError.Code == "" {
		return fmt.Sprintf("provider request failed with status %d", apiError.Status)
	}
	return apiError.Code + ": " + apiError.Message
}

// ListEntities requests one bounded Studio metadata page.
func (client *Client) ListEntities(ctx context.Context, query EntityQuery) (EntityPage, error) {
	if query.Limit < 1 || query.Limit > 200 || len(query.Cursor) > 2048 || len(query.Search) > 256 || hasControl(query.Cursor) || hasControl(query.Search) {
		return EntityPage{}, errors.New("metadata query exceeds Studio contract bounds")
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(query.Limit))
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	response, err := client.Do(ctx, http.MethodGet, "/studio/v1/metadata/entities?"+values.Encode(), nil)
	if err != nil {
		return EntityPage{}, err
	}
	var page EntityPage
	if err = decodeProviderResponse(response, &page); err != nil {
		return EntityPage{}, err
	}
	if len(page.Items) > query.Limit || len(page.NextCursor) > 2048 {
		return EntityPage{}, errors.New("provider returned an unbounded metadata page")
	}
	for _, entity := range page.Items {
		if err = validateEntity(entity); err != nil {
			return EntityPage{}, fmt.Errorf("provider returned invalid metadata: %w", err)
		}
	}
	return page, nil
}

// PutEntity performs an optimistic, idempotent Studio entity mutation.
func (client *Client) PutEntity(ctx context.Context, entity Entity, expectedVersion int64, idempotencyKey string) (Entity, error) {
	if err := validateEntity(entity); err != nil {
		return Entity{}, err
	}
	if expectedVersion < 0 || idempotencyKey == "" || len(idempotencyKey) > 256 || !safeToken.MatchString(idempotencyKey) {
		return Entity{}, errors.New("expected version and safe idempotency key are required")
	}
	body, err := json.Marshal(struct {
		Entity          Entity `json:"entity"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{Entity: entity, ExpectedVersion: expectedVersion, IdempotencyKey: idempotencyKey})
	if err != nil {
		return Entity{}, err
	}
	response, err := client.Do(ctx, http.MethodPut, "/studio/v1/metadata/entities/"+url.PathEscape(entity.Code), bytes.NewReader(body))
	if err != nil {
		return Entity{}, err
	}
	var updated Entity
	if err = decodeProviderResponse(response, &updated); err != nil {
		return Entity{}, err
	}
	if err = validateEntity(updated); err != nil {
		return Entity{}, fmt.Errorf("provider returned invalid metadata: %w", err)
	}
	return updated, nil
}

func decodeProviderResponse(response *http.Response, target any) error {
	if response == nil {
		return errors.New("provider returned no response")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("provider response is not application/json")
	}
	return decodeBoundedJSON(response.Body, maxMetadataResponse, target)
}

func decodeAPIError(response *http.Response) error {
	apiError := &APIError{Status: response.StatusCode}
	if err := decodeBoundedJSON(response.Body, maxErrorResponse, apiError); err != nil {
		return apiError
	}
	apiError.Code = safePublicText(apiError.Code, 128)
	apiError.Message = safePublicText(apiError.Message, 1024)
	apiError.RequestID = safePublicText(apiError.RequestID, 256)
	return apiError
}

func decodeBoundedJSON(reader io.Reader, limit int64, target any) error {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if limited.N <= 0 {
		return errors.New("provider response exceeds size limit")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("provider response contains trailing JSON")
	}
	return nil
}

func validateEntity(entity Entity) error {
	if !safePathValue(entity.Code, 256) || entity.Name == "" || len(entity.Name) > 512 || hasControl(entity.Name) || len(entity.Kind) > 128 || hasControl(entity.Kind) || entity.Version < 0 || len(entity.Fields) > 200 {
		return errors.New("entity exceeds Studio contract safety bounds")
	}
	for _, field := range entity.Fields {
		if !safePathValue(field.Code, 256) || field.DataType == "" || len(field.DataType) > 256 || hasControl(field.DataType) {
			return errors.New("field exceeds Studio contract safety bounds")
		}
	}
	return nil
}

func safePathValue(value string, limit int) bool {
	return value != "" && len(value) <= limit && !hasControl(value) && !strings.ContainsAny(value, "/\\?#")
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func safePublicText(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}
