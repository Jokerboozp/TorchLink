package model

import "errors"

var ErrNotFound = errors.New("not found")
var ErrResourceInUse = errors.New("resource is referenced")
var ErrInvalidIngress = errors.New("invalid ingress message")
