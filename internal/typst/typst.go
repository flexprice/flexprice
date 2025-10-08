package typst

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type Compiler interface {
	Compile(opts CompileOpts) (string, error)
	CompileToBytes(opts CompileOpts) ([]byte, error)
	CompileTemplate(templateName types.TemplateName, data []byte, opts ...CompileOptsBuilder) ([]byte, error)
	CleanupGeneratedFiles(files ...string)
}

// compiler represents a Typst document compiler
type compiler struct {
	// logger for logging
	logger *logger.Logger
	// Path to the typst binary
	binaryPath string
	// Directory where fonts are stored
	fontDir string
	// Directory where templates are stored
	templateDir string
	// Directory for output files
	outputDir string
}

// CompileOpts contains options for compiling a Typst document
type CompileOpts struct {
	// Input file path
	InputFile string
	// Output file name (optional, if not provided a temp file will be created)
	OutputFile string
	// Font paths to include
	FontDirs []string
	// Additional command-line arguments
	ExtraArgs []string
}

type CompileOptsBuilder func(c *CompileOpts)

func WithInputFile(inputFile string) CompileOptsBuilder {
	return func(c *CompileOpts) {
		c.InputFile = inputFile
	}
}

func WithOutputFile(outputFile string) CompileOptsBuilder {
	return func(c *CompileOpts) {
		c.OutputFile = outputFile
	}
}

func WithFontDirs(fontDirs ...string) CompileOptsBuilder {
	return func(c *CompileOpts) {
		c.FontDirs = fontDirs
	}
}

func WithExtraArgs(extraArgs ...string) CompileOptsBuilder {
	return func(c *CompileOpts) {
		c.ExtraArgs = extraArgs
	}
}

// NewCompiler creates a new Typst compiler
func NewCompiler(logger *logger.Logger, binaryPath, fontDir, templateDir, outputDir string) Compiler {
	return &compiler{
		logger:      logger,
		binaryPath:  binaryPath,
		fontDir:     fontDir,
		templateDir: templateDir,
		outputDir:   outputDir,
	}
}

// DefaultCompiler creates a compiler with default settings
func DefaultCompiler(logger *logger.Logger) Compiler {
	return &compiler{
		logger:      logger,
		binaryPath:  "typst",
		fontDir:     "assets/fonts",
		templateDir: "assets/typst-templates",
		outputDir:   os.TempDir(),
	}
}

// Compile compiles a Typst document to PDF
func (c *compiler) Compile(opts CompileOpts) (string, error) {
	// Determine output file path
	outputFile := filepath.Join(c.outputDir, opts.OutputFile)
	if opts.OutputFile == "" {
		tmpFilePath := filepath.Join(c.outputDir, fmt.Sprintf("typst-%d.pdf", time.Now().UnixMilli()))
		tmpFile, err := os.Create(tmpFilePath)
		if err != nil {
			return "", ierr.WithError(err).
				WithMessage("failed to create temporary output file").
				WithHint("template error").Mark(ierr.ErrSystem)
		}
		tmpFile.Close()
		outputFile = tmpFilePath
	}

	// Build font directories argument
	var fontDirs []string
	if c.fontDir != "" {
		fontDirs = append(fontDirs, c.fontDir)
	}
	fontDirs = append(fontDirs, opts.FontDirs...)

	// Build command
	args := []string{"compile", "--root", "/"}

	// Add font directories
	for _, dir := range fontDirs {
		args = append(args, "--font-path", dir)
	}

	// Add extra arguments
	args = append(args, opts.ExtraArgs...)

	// Add input and output files
	args = append(args, opts.InputFile, outputFile)

	c.logger.Debugf("Executing command to compile typst document: %s %v", c.binaryPath, args)

	cmd := exec.Command(c.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		c.logger.Errorf("Typst compilation failed: %s", stderr.String())
		return "", ierr.WithError(err).
			WithMessage("typst compilation failed").
			WithHint("typst error").
			WithReportableDetails(map[string]any{
				"stderr": stderr.String(),
			}).
			Mark(ierr.ErrSystem)
	}

	return outputFile, nil
}

// CompileToBytes compiles a Typst document and returns the PDF content as bytes
func (c *compiler) CompileToBytes(opts CompileOpts) ([]byte, error) {
	pdfPath, err := c.Compile(opts)
	if err != nil {
		return nil, err
	}
	defer os.Remove(pdfPath)
	return os.ReadFile(pdfPath)
}

// CompileTemplate compiles a Typst template with the provided data
// the data needs to be a valid JSON compatible with the template
// example:
//
//	data := "invoice-data={\"invoice_id\": \"1234567890\", \"invoice_number\": \"INV-1234567890\", \"customer_id\": \"1234567890\"}"
func (c *compiler) CompileTemplate(
	templateName types.TemplateName,
	data []byte,
	opts ...CompileOptsBuilder,
) ([]byte, error) {
	// Ensure template exists
	templatePath := filepath.Join(c.templateDir, templateName.String())
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return nil, ierr.WithError(err).
			WithMessagef("template not found: %s", templatePath).
			WithHint("template error").Mark(ierr.ErrSystem)
	}

	// create json file in temp dir
	jsonFile, err := os.Create(filepath.Join(c.outputDir, fmt.Sprintf("typst-%d.json", time.Now().UnixMilli())))
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to create temporary json file").
			WithHint("template error").Mark(ierr.ErrSystem)
	}

	// write data to json file
	if _, err := jsonFile.Write(data); err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to write data to json file").
			WithHint("template error").Mark(ierr.ErrSystem)
	}

	jsonFile.Close()

	// Compile the template
	compileOpts := CompileOpts{
		InputFile: templatePath,
		ExtraArgs: []string{"--input", fmt.Sprintf("path=%s", jsonFile.Name())},
	}

	defer os.Remove(jsonFile.Name())

	for _, opt := range opts {
		opt(&compileOpts)
	}

	return c.CompileToBytes(compileOpts)
}

// CleanupGeneratedFiles removes temporary files created during compilation
func (c *compiler) CleanupGeneratedFiles(files ...string) {
	for _, file := range files {
		if file != "" {
			os.Remove(file)
		}
	}
}

// CopyDir copies a directory recursively from src to dst
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !srcInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	err = os.MkdirAll(dst, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create destination directory if it doesn't exist
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}
