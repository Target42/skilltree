package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"skilltree/internal/store"
)

//go:embed templates/*.html static/*
var content embed.FS

func Templates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"levelMarks": store.LevelMarks,
	}).ParseFS(content, "templates/*.html")
}

func StaticHandler() http.Handler {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
