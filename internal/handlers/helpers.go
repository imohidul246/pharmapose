package handlers

import (
	"errors"
	"time"
)

var (
	errNameRequired = errors.New("name is required")
	errLimitInvalid = errors.New("limit must be a positive integer")
)

func timeNow() time.Time { return time.Now().UTC() }
