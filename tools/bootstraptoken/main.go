package main

import (
	"fmt"
	"os"

	"github.com/romedrori/edge-direct-demo/internal/httpapi"
)

// Prints the bootstrap token a device with the given tenant + serial would
// have been flashed with at manufacturing. Handy for local demos.
func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: bootstraptoken <secret> <tenant> <serial>")
		os.Exit(2)
	}
	fmt.Println(httpapi.BootstrapToken(os.Args[1], os.Args[2], os.Args[3]))
}
