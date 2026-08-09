package middleware

import (
	"errors"
	"strings"

	"koi-server/packages/socketio/contracts"

	"github.com/goravel/framework/facades"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

type TokenValidator func(token string) bool

var defaultValidator TokenValidator = func(token string) bool {
	return strings.TrimSpace(token) != ""
}

func SetTokenValidator(validator TokenValidator) {
	defaultValidator = validator
}

func AuthMiddleware() contracts.Middleware {
	return contracts.MiddlewareFunc(func(socket *socketio.Socket, next func() error) error {
		handshake := socket.Handshake()
		token := ""
		if handshake != nil {
			if t, ok := handshake.Auth["token"].(string); ok {
				token = t
			}
		}

		if token == "" {
			facades.Log().Error("Socket connection failed: missing token")
			return errors.New("authentication required")
		}

		if defaultValidator != nil && !defaultValidator(token) {
			facades.Log().Error("Socket connection failed: invalid token")
			return errors.New("invalid token")
		}

		facades.Log().Info("Socket connection authenticated: " + string(socket.Id()))
		return next()
	})
}

func LoggerMiddleware() contracts.Middleware {
	return contracts.MiddlewareFunc(func(socket *socketio.Socket, next func() error) error {
		facades.Log().Info("Socket event received from: " + string(socket.Id()))
		err := next()
		if err != nil {
			facades.Log().Error("Socket event error: " + err.Error())
		} else {
			facades.Log().Info("Socket event processed successfully")
		}
		return err
	})
}
