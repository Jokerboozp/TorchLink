package connector

import "errors"

// ErrAuthentication marks a typed credential rejection; arbitrary broker or
// driver text is never interpreted as proof of an authentication failure.
var ErrAuthentication = errors.New("connector authentication failed")
