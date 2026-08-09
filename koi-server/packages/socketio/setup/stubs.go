package main

import "strings"

func config(configPackage string, facadesImport, facadesPackage string) string {
	content := `package DummyPackage

import (
	"DummyFacadesImport"
)

func init() {
	config := DummyFacadesPackage.Config()
	config.Add("socketio", map[string]any{
		
	})
}
`
	content = strings.ReplaceAll(content, "DummyPackage", configPackage)
	content = strings.ReplaceAll(content, "DummyFacadesImport", facadesImport)
	content = strings.ReplaceAll(content, "DummyFacadesPackage", facadesPackage)

	return content
}
