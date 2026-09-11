// Command svgify converts raster images into tightly cropped SVG vector graphics.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, "svgify:", err)
		}
		os.Exit(exitCode(err))
	}
}
