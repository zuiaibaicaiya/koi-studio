package facades

import (
	"koi-server/packages/socketio/contracts"
)

func Socketio() contracts.Socketio {
	instance, err := App().Make("socketio")
	if err != nil {
		panic("Failed to make socketio: " + err.Error())
	}
	return instance.(contracts.Socketio)
}
