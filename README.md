# watermarker

watermarker is a basic Go tool for assembling a directory of image files into a PDF, and optionally resizing and watermarking the pages. This is largely used to convert directories of document scans into a distributable PDF.

[ImageMagick](https://imagemagick.org) is used under the hood for mutating and watermarking the page files, and [img2pdf](https://pypi.org/project/img2pdf/) for re-assembling into a PDF.

## Build

1. `git clone https://github.com/Cryptkeeper/watermarker`
2. `go build .`

## Usage

watermarker requires an input directory to search for image files, and an output path for the assembled PDF. By default, watermarker will attempt to process any jpg or jpeg files in the provided search directory, but file extensions can be specified using `-ext`.

`watermarker -dir ~/my-files -output ~/doc.pdf`

To watermark the pages, provide an image path to `-watermark ~/watermark.png` and it will be overlaid in the top left of the page automatically. Use `-size` to adjust the scale of the watermark.

See `watermarker -h` for additional arguments.

## Warning

This is a specialized one-off tool that I opted to publish so it wouldn't be hidden away on a hard drive somewhere. It makes several assumptions about the dimensions (and DPI) of images, file name formats, and interacts with the underlying tools using command executions. This means it may not do what you want, and if you have too many files (high hundreds), it may not do anything at all.
