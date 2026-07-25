// Command server hosts the Gemeten Stad API and the SvelteKit SPA. It is
// layered controller → service → repository (see docs/IMPLEMENTATION_PLAN.md §5)
// and is deployed separately from the batch pipeline.
//
// This is a placeholder entry point; the HTTP surface is built in a later phase.
package main

import "log"

func main() {
	log.Println("server: not yet implemented")
}
