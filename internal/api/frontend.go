package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed frontend
var frontendFiles embed.FS

func frontendHandler() http.Handler {
	sub, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
