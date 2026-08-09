package handler

import (
	"fmt"
	"log"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

func ConnectionHandler(socket *socketio.Socket, args ...any) error {
	log.Printf("Client connected: %s\n", socket.Id())
	socket.Emit("welcome", "Welcome to Socket.IO server")
	return nil
}

func DisconnectHandler(socket *socketio.Socket, args ...any) error {
	reason := "unknown"
	if len(args) > 0 {
		reason = fmt.Sprintf("%v", args[0])
	}
	log.Printf("Client disconnected: %s, reason: %s\n", socket.Id(), reason)
	return nil
}

func ErrorHandler(socket *socketio.Socket, args ...any) error {
	if len(args) > 0 {
		log.Printf("Socket error: %v\n", args[0])
	}
	return nil
}

func MessageHandler(socket *socketio.Socket, args ...any) error {
	if len(args) > 0 {
		message := args[0]
		log.Printf("Received message: %v\n", message)
		socket.Emit("message", message)
	}
	return nil
}

func ChatHandler(socket *socketio.Socket, args ...any) error {
	if len(args) > 0 {
		message := args[0]
		log.Printf("Received chat message: %v\n", message)
		socket.Emit("message", message)
	}
	return nil
}
