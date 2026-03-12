package models

import "errors"

var (
	ErrMissingAppNameOrUserID            = errors.New("app_name and user_id are required")
	ErrMissingAppNameOrUserIDOrSessionID = errors.New(
		"app_name, user_id and session_id are required",
	)
)
