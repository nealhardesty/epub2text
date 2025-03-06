package main

import (
	"archive/zip"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Package metadata structure
type Package struct {
	XMLName  xml.Name `xml:"package"`
	Manifest Manifest `xml:"manifest"`
	Spine    Spine    `xml:"spine"`
}

type Manifest struct {
	Items []Item `xml:"item"`
}

type Item struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type Spine struct {
	ItemRefs []ItemRef `xml:"itemref"`
}

type ItemRef struct {
	IDRef string `xml:"idref,attr"`
}

// Container metadata structure
type Container struct {
	XMLName   xml.Name  `xml:"container"`
	RootFiles RootFiles `xml:"rootfiles"`
}

type RootFiles struct {
	RootFile []RootFile `xml:"rootfile"`
}

type RootFile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

func main() {
	// Define command line flags
	inputFile := flag.String("input", "", "Path to EPUB file (required)")
	outputFile := flag.String("output", "", "Path to output text file (default: derived from input filename)")
	flag.Parse()

	// Check if input file is provided
	if *inputFile == "" {
		fmt.Println("Error: input file is required")
		flag.Usage()
		os.Exit(1)
	}

	// Set default output file if not provided
	if *outputFile == "" {
		baseName := filepath.Base(*inputFile)
		ext := filepath.Ext(baseName)
		*outputFile = strings.TrimSuffix(baseName, ext) + ".txt"
	}

	fmt.Printf("Converting %s to %s\n", *inputFile, *outputFile)

	// Start the conversion process
	err := convertEpubToText(*inputFile, *outputFile)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Conversion completed successfully")
}

func convertEpubToText(epubPath, txtPath string) error {
	// Open the EPUB file (which is a ZIP archive)
	reader, err := zip.OpenReader(epubPath)
	if err != nil {
		return fmt.Errorf("failed to open EPUB file: %w", err)
	}
	defer reader.Close()

	// Find and parse the container.xml file to get the OPF file
	var containerFile *zip.File
	for _, file := range reader.File {
		if file.Name == "META-INF/container.xml" {
			containerFile = file
			break
		}
	}
	if containerFile == nil {
		return fmt.Errorf("container.xml file not found in EPUB")
	}

	// Parse container.xml to find the OPF file
	container, err := parseContainer(containerFile)
	if err != nil {
		return err
	}

	if len(container.RootFiles.RootFile) == 0 {
		return fmt.Errorf("no rootfile found in container.xml")
	}

	// Get the OPF file path
	opfPath := container.RootFiles.RootFile[0].FullPath

	// Find the OPF file
	var opfFile *zip.File
	for _, file := range reader.File {
		if file.Name == opfPath {
			opfFile = file
			break
		}
	}
	if opfFile == nil {
		return fmt.Errorf("OPF file not found at path: %s", opfPath)
	}

	// Parse the OPF file to get content ordering
	pkg, err := parsePackage(opfFile)
	if err != nil {
		return err
	}

	// Create a base directory for resolving relative paths
	baseDir := filepath.Dir(opfPath)

	// Create a map of ID to file path
	idToPath := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		// Only include HTML content
		if strings.Contains(item.MediaType, "html") || strings.Contains(item.MediaType, "xhtml") {
			idToPath[item.ID] = filepath.Join(baseDir, item.Href)
		}
	}

	// Get ordered content files
	var contentPaths []string
	for _, itemRef := range pkg.Spine.ItemRefs {
		if path, ok := idToPath[itemRef.IDRef]; ok {
			contentPaths = append(contentPaths, path)
		}
	}

	// Extract all content files
	var textContent strings.Builder
	for _, contentPath := range contentPaths {
		// Find the file in the ZIP
		var contentFile *zip.File
		for _, file := range reader.File {
			// Normalize paths for comparison
			normalizedPath := filepath.ToSlash(file.Name)
			normalizedContentPath := filepath.ToSlash(contentPath)
			if normalizedPath == normalizedContentPath {
				contentFile = file
				break
			}
		}

		if contentFile == nil {
			fmt.Printf("Warning: content file not found: %s\n", contentPath)
			continue
		}

		// Extract text from this content file
		content, err := extractTextFromHTMLFile(contentFile)
		if err != nil {
			fmt.Printf("Warning: error processing %s: %v\n", contentPath, err)
			continue
		}

		textContent.WriteString(content)
		textContent.WriteString("\n\n")
	}

	// Write the text content to the output file
	err = os.WriteFile(txtPath, []byte(textContent.String()), 0644)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

func parseContainer(containerFile *zip.File) (*Container, error) {
	reader, err := containerFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open container.xml: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read container.xml: %w", err)
	}

	var container Container
	err = xml.Unmarshal(data, &container)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container.xml: %w", err)
	}

	return &container, nil
}

func parsePackage(opfFile *zip.File) (*Package, error) {
	reader, err := opfFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open OPF file: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read OPF file: %w", err)
	}

	var pkg Package
	err = xml.Unmarshal(data, &pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OPF file: %w", err)
	}

	return &pkg, nil
}

func extractTextFromHTMLFile(htmlFile *zip.File) (string, error) {
	reader, err := htmlFile.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open HTML file: %w", err)
	}
	defer reader.Close()

	// Parse HTML
	doc, err := html.Parse(reader)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract text
	var textBuilder strings.Builder
	extractText(doc, &textBuilder)

	// Clean up the text
	text := textBuilder.String()

	// Remove excessive whitespace
	space := regexp.MustCompile(`\s+`)
	text = space.ReplaceAllString(text, " ")

	// Remove leading/trailing whitespace from lines
	var cleanLines []string
	for _, line := range strings.Split(text, "\n") {
		cleanLine := strings.TrimSpace(line)
		if cleanLine != "" {
			cleanLines = append(cleanLines, cleanLine)
		}
	}

	return strings.Join(cleanLines, "\n"), nil
}

func extractText(n *html.Node, builder *strings.Builder) {
	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			builder.WriteString(text)
			builder.WriteString(" ")
		}
	}

	// Check if this node is a block element that should add a line break
	if n.Type == html.ElementNode {
		switch n.Data {
		case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "br", "hr":
			builder.WriteString("\n")
		}
	}

	// Process child nodes
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractText(c, builder)
	}

	// Add additional line breaks after certain elements
	if n.Type == html.ElementNode {
		switch n.Data {
		case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li":
			builder.WriteString("\n")
		}
	}
}
