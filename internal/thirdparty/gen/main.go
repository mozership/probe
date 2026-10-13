// Command gen writes the licenses of what is built into the probe
// executable to a file, for a release archive to carry:
//
//	go run ./internal/thirdparty/gen -o THIRD_PARTY_LICENSES
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mozership/probe/internal/thirdparty"
)

func main() {
	out := flag.String("o", "", "the file to write; the standard output when empty")
	pkg := flag.String("pkg", "./cmd/probe", "the package the executable is built from")
	platforms := flag.String("platforms", "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64", "the platforms it is built for, as os/arch,...")
	flag.Parse()

	if err := run(*out, *pkg, strings.Split(*platforms, ",")); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
}

func run(out, pkg string, platforms []string) error {
	modules, err := thirdparty.Modules(".", pkg, platforms)
	if err != nil {
		return err
	}
	// Nothing is written unless all of it can be.
	var b bytes.Buffer
	if err := thirdparty.Write(&b, modules); err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.Write(b.Bytes())
		return err
	}
	return os.WriteFile(out, b.Bytes(), 0o644)
}
