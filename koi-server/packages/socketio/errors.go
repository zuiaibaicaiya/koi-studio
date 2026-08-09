package socketio

import "errors"

var (
	ErrInvalidConfig     = errors.New("invalid socket.io config")
	ErrConnectionFailed  = errors.New("socket connection failed")
	ErrNamespaceNotFound = errors.New("namespace not found")
	ErrInvalidNamespace  = errors.New("invalid namespace")
	ErrInvalidRoom       = errors.New("invalid room")
	ErrHandlerNotFound   = errors.New("event handler not found")
	ErrMiddlewareFailed  = errors.New("middleware execution failed")
)
