package main

import (
	"fmt"
	"os"

	"cybercalc/internal/migrationcheck"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: migrationcheck <migrations-dir> <checksums-file>")
		os.Exit(2)
	}
	if err := migrationcheck.Check(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "migration check:", err)
		os.Exit(1)
	}
	fmt.Println("migration check: OK")
}
