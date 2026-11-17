package session

import "errors"

// ErrNotFound means the session does not exist, has expired, or was revoked.
var ErrNotFound = errors.New("session not found")

var ErrUnavailable = errors.New("session storage unavailable")
