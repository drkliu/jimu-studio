package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const DisableUserConfirmation = "DISABLE_USER"

type User struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email,omitempty"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Version     int64    `json:"version"`
}

type Role struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	System      bool   `json:"system"`
	Version     int64  `json:"version"`
}

type UserPage struct {
	Items      []User `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
type RolePage struct {
	Items      []Role `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
type IdentityQuery struct {
	Cursor string
	Limit  int
	Search string
}

func (client *Client) ListUsers(ctx context.Context, query IdentityQuery) (UserPage, error) {
	var page UserPage
	err := client.identityList(ctx, "/studio/v1/identity/users", query, &page)
	if err == nil {
		if len(page.Items) > query.Limit || len(page.NextCursor) > 2048 {
			return UserPage{}, errors.New("provider returned unbounded users")
		}
		for _, item := range page.Items {
			if err = validateUser(item); err != nil {
				return UserPage{}, fmt.Errorf("invalid provider user: %w", err)
			}
		}
	}
	return page, err
}

func (client *Client) ListRoles(ctx context.Context, query IdentityQuery) (RolePage, error) {
	var page RolePage
	err := client.identityList(ctx, "/studio/v1/identity/roles", query, &page)
	if err == nil {
		if len(page.Items) > query.Limit || len(page.NextCursor) > 2048 {
			return RolePage{}, errors.New("provider returned unbounded roles")
		}
		for _, item := range page.Items {
			if err = validateRole(item); err != nil {
				return RolePage{}, fmt.Errorf("invalid provider role: %w", err)
			}
		}
	}
	return page, err
}

func (client *Client) identityList(ctx context.Context, path string, query IdentityQuery, target any) error {
	if query.Limit < 1 || query.Limit > 200 || len(query.Cursor) > 2048 || len(query.Search) > 256 || hasControl(query.Cursor) || hasControl(query.Search) {
		return errors.New("identity query exceeds bounds")
	}
	values := url.Values{"limit": {strconv.Itoa(query.Limit)}}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	return client.identityRequest(ctx, http.MethodGet, path+"?"+values.Encode(), nil, target)
}

func (client *Client) CreateUser(ctx context.Context, user User, key string) (User, error) {
	if !safePathValue(user.ID, 256) || !boundedIdentityText(user.DisplayName, 512) || !boundedIdentityText(user.Email, 512) || !safeIdentityKey(key) {
		return User{}, errors.New("safe user and idempotency key required")
	}
	body := struct {
		DisplayName    string `json:"display_name"`
		Email          string `json:"email"`
		ID             string `json:"id"`
		IdempotencyKey string `json:"idempotency_key"`
	}{user.DisplayName, user.Email, user.ID, key}
	var result User
	err := client.identityRequest(ctx, http.MethodPost, "/studio/v1/identity/users", body, &result)
	if err == nil {
		err = validateUser(result)
	}
	return result, err
}

func (client *Client) SetUserStatus(ctx context.Context, id, status string, version int64, key string) (User, error) {
	if !safePathValue(id, 256) || (status != "active" && status != "disabled") || version < 0 || !safeIdentityKey(key) {
		return User{}, errors.New("safe status mutation required")
	}
	body := struct {
		Confirmation    string `json:"confirmation"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
		Status          string `json:"status"`
	}{DisableUserConfirmation, version, key, status}
	var result User
	err := client.identityRequest(ctx, http.MethodPatch, "/studio/v1/identity/users/"+url.PathEscape(id)+"/status", body, &result)
	if err == nil {
		err = validateUser(result)
	}
	return result, err
}

func (client *Client) CreateRole(ctx context.Context, role Role, key string) (Role, error) {
	if !safePathValue(role.Key, 256) || !boundedIdentityText(role.DisplayName, 512) || !safeIdentityKey(key) {
		return Role{}, errors.New("safe role mutation required")
	}
	body := struct {
		DisplayName    string `json:"display_name"`
		IdempotencyKey string `json:"idempotency_key"`
		Key            string `json:"key"`
	}{role.DisplayName, key, role.Key}
	var result Role
	err := client.identityRequest(ctx, http.MethodPost, "/studio/v1/identity/roles", body, &result)
	if err == nil {
		err = validateRole(result)
	}
	return result, err
}

func (client *Client) UpdateRole(ctx context.Context, roleKey, displayName string, version int64, key string) (Role, error) {
	if !safePathValue(roleKey, 256) || !boundedIdentityText(displayName, 512) || version < 0 || !safeIdentityKey(key) {
		return Role{}, errors.New("safe role update required")
	}
	body := struct {
		DisplayName     string `json:"display_name"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{displayName, version, key}
	var result Role
	err := client.identityRequest(ctx, http.MethodPatch, "/studio/v1/identity/roles/"+url.PathEscape(roleKey), body, &result)
	if err == nil {
		err = validateRole(result)
	}
	return result, err
}

func (client *Client) ChangeUserRole(ctx context.Context, userID, roleKey string, version int64, key string, assign bool) (User, error) {
	if !safePathValue(userID, 256) || !safePathValue(roleKey, 256) || version < 0 || !safeIdentityKey(key) {
		return User{}, errors.New("safe role assignment required")
	}
	method := http.MethodDelete
	if assign {
		method = http.MethodPut
	}
	body := struct {
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{version, key}
	var result User
	err := client.identityRequest(ctx, method, "/studio/v1/identity/users/"+url.PathEscape(userID)+"/roles/"+url.PathEscape(roleKey), body, &result)
	if err == nil {
		err = validateUser(result)
	}
	return result, err
}

func (client *Client) identityRequest(ctx context.Context, method, path string, body, target any) error {
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	response, err := client.Do(ctx, method, path, reader)
	if err != nil {
		return err
	}
	return decodeProviderResponse(response, target)
}

func validateUser(user User) error {
	if !safePathValue(user.ID, 256) || !boundedIdentityText(user.DisplayName, 512) || len(user.Email) > 512 || hasControl(user.Email) || (user.Status != "active" && user.Status != "disabled") || user.Version < 0 || len(user.Roles) > 200 {
		return errors.New("user exceeds bounds")
	}
	for _, role := range user.Roles {
		if !safePathValue(role, 256) {
			return errors.New("user role exceeds bounds")
		}
	}
	return nil
}
func validateRole(role Role) error {
	if !safePathValue(role.Key, 256) || !boundedIdentityText(role.DisplayName, 512) || role.Version < 0 {
		return errors.New("role exceeds bounds")
	}
	return nil
}
func boundedIdentityText(value string, limit int) bool {
	return value != "" && len(value) <= limit && !hasControl(value)
}
func safeIdentityKey(value string) bool {
	return value != "" && len(value) <= 256 && safeToken.MatchString(value)
}
