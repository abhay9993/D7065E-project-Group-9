package buildingsim

import "embed"

//go:generate go run ./cmd/buildweb

//go:embed all:data
var DataFS embed.FS

//go:embed all:web
var WebFS embed.FS
