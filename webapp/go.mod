// This file exists only to mark webapp/ as a Go module boundary, so the
// root module's `./...` (build/test/lint) does not descend into
// webapp/node_modules, which can ship incidental .go files as part of
// npm packages' transitive dependencies.
module github.com/Blogem/gemeten-stad/webapp

go 1.26.1
