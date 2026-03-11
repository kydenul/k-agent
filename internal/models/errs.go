package models

import "errors"

var ErrMissingAppNameOrUserID = errors.New("app_name and user_id are required")
