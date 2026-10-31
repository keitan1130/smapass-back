package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"smapass-back/internal/apidoc"
)

func main() {
	check := flag.Bool("check", false, "fail if docs/openapi.json needs regeneration")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	source, err := os.ReadFile("docs/openapi.base.json")
	if err != nil {
		return err
	}
	generated, err := apidoc.Generate(source)
	if err != nil {
		return err
	}
	if check {
		current, err := os.ReadFile("docs/openapi.json")
		if err != nil {
			return err
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("OpenAPI is out of date: run make generate")
		}
		return nil
	}
	return os.WriteFile("docs/openapi.json", generated, 0644)
}
