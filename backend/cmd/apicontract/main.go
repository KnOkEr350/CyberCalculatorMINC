package main

import (
	"fmt"
	"os"

	"cybercalc/internal/apicontract"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: apicontract <openapi-v1.json> <modules-dir>")
		os.Exit(2)
	}
	document, err := apicontract.Load(os.Args[1])
	if err == nil {
		err = apicontract.Validate(document)
	}
	var registered []apicontract.Operation
	if err == nil {
		registered, err = apicontract.DiscoverModuleRoutes(os.Args[2])
	}
	if err == nil {
		err = apicontract.CompareRoutes(apicontract.Operations(document), registered)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "API contract check:", err)
		os.Exit(1)
	}
	fmt.Printf("API contract check: OK (%d operations)\n", len(registered))
}
