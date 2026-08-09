package main

import (
	"koi-server/bootstrap"
)

func main() {
	app := bootstrap.Boot()

	app.Start()
}
