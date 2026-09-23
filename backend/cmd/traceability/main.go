// Command traceability печатает матрицу «задача плана → код, тесты, документы».
//
//	go run ./cmd/traceability > ../docs/TRACEABILITY.md
//
// Запускается из каталога backend; корень репозитория — на уровень выше.
package main

import (
	"fmt"
	"os"

	"cybercalc/internal/traceability"
)

func main() {
	root := ".."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	rows, err := traceability.Build(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "traceability:", err)
		os.Exit(1)
	}
	fmt.Print(traceability.Markdown(rows))
	if problems := traceability.Problems(root, rows); len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "traceability:", problem)
		}
		os.Exit(3)
	}
}
