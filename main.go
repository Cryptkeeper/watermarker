package main

import (
	"flag"
	"fmt"
	"golang.org/x/exp/slices"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	directorySearchPath   string // path to search for input files
	fileExtensions        string // comma-separated list of file extensions to search for
	watermarkFilePath     string // path to watermark image
	watermarkImageScale   int    // scale factor for watermark image
	outputDimensionHeight int    // output page height in pixels @ 150 DPI
	outputDimensionWidth  int    // output page width in pixels @ 150 DPI
	outputFilePath        string // output file path for generated PDF
	workDirectory         string // directory for writing temporary files
)

// parseArgs parses command line argument flags and configures global variables
func parseArgs() {
	flag.StringVar(&directorySearchPath, "dir", "", "Input directory search path for pages")
	flag.StringVar(&fileExtensions, "ext", ".jpg,.jpeg", "A comma-separated list of supported file extensions")
	flag.StringVar(&watermarkFilePath, "watermark", "", "Watermark file path")
	flag.IntVar(&watermarkImageScale, "size", 4, "Watermark image size scale")
	flag.IntVar(&outputDimensionHeight, "height", 1500, "Output page height in pixels @ 150 DPI")
	flag.IntVar(&outputDimensionWidth, "width", 1500, "Output page width in pixels @ 150 DPI")
	flag.StringVar(&outputFilePath, "output", "", "Output file path")
	flag.StringVar(&workDirectory, "workdir", ".watermarker-workdir", "Work directory for temporary files")

	flag.Parse()
}

// walkDirectorySearchPath walks the directory search path and sends matching file paths to the ingest channel
// the ingest channel is closed when the walk is complete
func walkDirectorySearchPath(ingest chan string) {
	exts := strings.Split(fileExtensions, ",")
	if err := filepath.WalkDir(directorySearchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// skip directories
		if d.IsDir() {
			return nil
		}

		// skip files that don't match the specified extensions
		if !slices.Contains(exts, filepath.Ext(path)) {
			fmt.Println("skipping:", path)
			return nil
		}

		ingest <- path
		return nil
	}); err != nil {
		panic(err)
	}
	close(ingest)
}

// printCommandOutput prints the output of an exec.Command call to stdout if an error occurred
func printCommandOutput(b []byte, err error) {
	if err != nil {
		fmt.Println(string(b))
	}
}

// page represents a matched file in the search directory that has had its name parsed for a page number
type page struct {
	filepath string
	number   int
	tmpPath  string
}

// createTempFile creates a temporary file in the work directory with the same extension as the original file, and
// assigns the path to the tmpPath field of the page struct for later use
func (p *page) createTempFile() error {
	ext := filepath.Ext(p.filepath)

	// ensure work directory exists
	_ = os.Mkdir(workDirectory, os.ModePerm)

	f, err := os.CreateTemp(workDirectory, "watermarker-*"+ext)
	if err != nil {
		return err
	}
	_ = f.Close() // no need to keep handle, we just want the file pattern generated and ready

	p.tmpPath = f.Name()

	return nil
}

// convert converts the original file to a new file with the specified dimensions and density
func (p *page) convert() error {
	dims := fmt.Sprintf("%dx%d", outputDimensionWidth, outputDimensionHeight)
	b, err := exec.Command("convert", p.filepath, "-auto-orient", "-resize", dims, "-density", "150", "-strip", p.tmpPath).CombinedOutput()

	printCommandOutput(b, err)

	return err
}

// watermark applies a watermark to the temporary file using the specified watermark image
func (p *page) watermark() error {
	b, err := exec.Command("magick",
		p.tmpPath,
		"-colorspace", "sRGB",
		"-set", "option:WMSIZE", fmt.Sprintf("%%[fx:w/%d]x%%[fx:h/%d]", watermarkImageScale, watermarkImageScale),
		"(", // subcommand start
		watermarkFilePath,
		"-resize",
		"%[WMSIZE]",
		")", // subcommand end
		"-geometry", "+25+25",
		"-composite",
		p.tmpPath,
	).CombinedOutput()

	printCommandOutput(b, err)

	return err
}

// generate generates a processed temporary file for the page, converting it to the specified dimensions and density,
// and applying a watermark if enabled
func (p *page) generate() error {
	if err := p.createTempFile(); err != nil {
		return err
	} else if err := p.convert(); err != nil {
		return err
	} else if len(watermarkFilePath) > 0 {
		if err := p.watermark(); err != nil {
			return err
		}
	}
	return nil
}

var pageNumberRegex = regexp.MustCompile(`.+(\d+)\.?`)

// ingestPages walks the directory search path and ingests all matched files into a slice of page metadata structs
func ingestPages() ([]page, error) {
	ingest := make(chan string)

	go walkDirectorySearchPath(ingest)

	var pages []page

	for path := range ingest {
		filename := filepath.Base(path)

		// extract page number from filename
		matches := pageNumberRegex.FindStringSubmatch(filename)
		if len(matches[1]) == 0 {
			return nil, fmt.Errorf("no page number found in filename: %s", filename)
		}

		number, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, err
		}

		pages = append(pages, page{
			filepath: path,
			number:   number,
		})

		fmt.Printf("found page: %s\n", path)
	}

	// sort pages by parsed page number
	slices.SortFunc(pages, func(i, j page) int {
		return i.number - j.number
	})

	return pages, nil
}

// genProcessedPages generates processed temporary files for each page in the slice of page metadata structs, updating
// the tmpPath field with the path to the generated file
func genProcessedPages(pages []page) {
	// TODO: this uses a go routine per file, which could be inefficient for large numbers of files
	// consider using a worker pool pattern to limit the number of concurrent operations, but even several hundred
	// go routines should be fine for most use cases
	wg := sync.WaitGroup{}
	wg.Add(len(pages))

	for i := range pages {
		go func(p *page) {
			if err := p.generate(); err != nil {
				fmt.Printf("error processing page: %s\n", p.filepath)
			} else {
				fmt.Printf("processed page: %s\n", p.filepath)
			}

			defer wg.Done()
		}(&pages[i])
	}

	wg.Wait()
}

// bundlePages bundles the processed temporary files into a single PDF file written to the specified output path
func bundlePages(pages []page) error {
	// TODO: this operates by passing all files to img2pdf at once, which WILL be inefficient for large numbers of files
	// and may potentially be impacted by platform limits on command line length
	files := make([]string, len(pages))
	for i, p := range pages {
		files[i] = p.tmpPath
	}

	b, err := exec.Command("img2pdf", append([]string{
		"--output", outputFilePath,
	}, files...)...).CombinedOutput()

	printCommandOutput(b, err)

	return err
}

func main() {
	parseArgs()

	if len(directorySearchPath) == 0 || len(outputFilePath) == 0 {
		fmt.Println("Usage: watermarker -dir <directory> -output <output path> [options]")
		flag.PrintDefaults()
		return
	}

	pages, err := ingestPages()
	if err != nil {
		fmt.Printf("error ingesting pages: %v\n", err)
		return
	}

	// generate processed temp files for each page
	genProcessedPages(pages)

	// bundle processed pages into a single PDF
	fmt.Println("bundling...")

	if err := bundlePages(pages); err != nil {
		fmt.Printf("error bundling pages: %v\n", err)
		return
	}

	// attempt to clear leftover temp files
	for _, p := range pages {
		_ = os.Remove(p.tmpPath)
	}
}
