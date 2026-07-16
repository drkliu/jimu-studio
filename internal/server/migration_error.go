package server

import (
	"errors"
	"net/http"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

func (server *authenticatedServer) renderMigrationError(response http.ResponseWriter, session auth.SessionView, err error) {
	status := http.StatusBadGateway
	message := "The provider could not complete this migration operation."
	requestID := ""
	var apiError *provider.APIError
	if errors.As(err, &apiError) {
		if apiError.Status >= 400 && apiError.Status <= 599 {
			status = apiError.Status
		}
		requestID = apiError.RequestID
		switch status {
		case http.StatusBadRequest:
			message = "The provider rejected this migration request."
		case http.StatusUnauthorized, http.StatusForbidden:
			message = "The provider denied this migration operation."
		case http.StatusNotFound:
			message = "The provider could not find the requested migration resource."
		}
	}
	renderTemplate(response, status, metadataErrorTemplate, metadataErrorData{
		Session: session, Navigation: allowedNavigation(session), Title: http.StatusText(status), Message: message, RequestID: requestID,
	})
}
