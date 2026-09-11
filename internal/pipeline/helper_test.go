package pipeline_test

import (
	"image/jpeg"
	"io"

	"github.com/mdbtq/svgify/internal/testfixtures"
)

func jpegEncode(w io.Writer, f testfixtures.Fixture) error {
	return jpeg.Encode(w, f.Img, &jpeg.Options{Quality: 95})
}
