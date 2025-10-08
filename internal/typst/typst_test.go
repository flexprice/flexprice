package typst

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type TypstCompilerSuite struct {
	suite.Suite
	logger      *logger.Logger
	tempDir     string
	templateDir string
	sampleFile  string
	fontsDir    string
	outputDir   string
	compiler    Compiler
}

func TestTypstCompiler(t *testing.T) {
	suite.Run(t, new(TypstCompilerSuite))
}

func (s *TypstCompilerSuite) SetupTest() {
	// Check if typst is available in the system
	if _, err := exec.LookPath("typst"); err != nil {
		s.T().Skip("Skipping tests because typst is not available in the system")
		return
	}

	// Create a logger
	var err error
	s.logger, err = logger.NewLogger(config.GetDefaultConfig())
	s.Require().NoError(err)
	// Create temporary directories
	s.tempDir, err = os.MkdirTemp("", "typst-test-*")
	s.Require().NoError(err)

	// Create template directory
	s.templateDir = filepath.Join(s.tempDir, "templates")
	err = os.MkdirAll(s.templateDir, 0755)
	s.Require().NoError(err)

	// Create fonts directory
	s.fontsDir = filepath.Join(s.tempDir, "fonts")
	err = os.MkdirAll(s.fontsDir, 0755)
	s.Require().NoError(err)

	// copy templates from ../../assets/typst-templates dir to temp dir
	currentDir, err := os.Getwd()
	s.Require().NoError(err)
	err = CopyDir(filepath.Join(currentDir, "../../assets", "typst-templates"), s.templateDir)
	s.Require().NoError(err)

	// copy fonts from ../../assets/fonts dir to temp fonts dir
	err = CopyDir(filepath.Join(currentDir, "../../assets", "fonts"), s.fontsDir)
	s.Require().NoError(err)

	// Create a sample Typst file
	s.sampleFile = filepath.Join(s.templateDir, "sample.typ")
	err = os.WriteFile(s.sampleFile, []byte(`#set page(width: 10cm, height: 5cm)
#set text(font: "Inter")
Hello, #input("name")!`), 0644)
	s.Require().NoError(err)

	// Create output directory
	s.outputDir = filepath.Join(s.tempDir, "output")
	err = os.MkdirAll(s.outputDir, 0755)
	s.Require().NoError(err)

	// Initialize the compiler with test paths
	s.compiler = NewCompiler(
		s.logger,
		"typst",
		s.fontsDir,
		s.templateDir,
		s.outputDir,
	)
}

func (s *TypstCompilerSuite) TearDownTest() {
	// Clean up the temporary directory
	os.RemoveAll(s.tempDir)
}

func (s *TypstCompilerSuite) TestNewCompiler() {
	// Test that NewCompiler returns a non-nil compiler with correct settings
	c := NewCompiler(s.logger, "typst", s.fontsDir, s.templateDir, s.outputDir)
	s.NotNil(c)

	// Check that the fields are set properly using reflection
	compiler := c.(*compiler)
	s.Equal("typst", compiler.binaryPath)
	s.Equal(s.fontsDir, compiler.fontDir)
	s.Equal(s.templateDir, compiler.templateDir)
	s.Equal(s.outputDir, compiler.outputDir)
}

func (s *TypstCompilerSuite) TestBasicTypstCompilation() {
	// Create a simple Typst file
	testInputPath := filepath.Join(s.tempDir, "basic.typ")
	err := os.WriteFile(testInputPath, []byte("Hello, World!"), 0644)
	s.Require().NoError(err)

	// Compile the simple Typst file
	outputPath := filepath.Join(s.tempDir, "output", "basic.pdf")
	// create the output file
	err = os.WriteFile(outputPath, []byte(""), 0644)
	s.Require().NoError(err)

	result, err := s.compiler.Compile(CompileOpts{
		InputFile:  testInputPath,
		OutputFile: "basic.pdf",
	})

	// Ensure compilation does not fail
	s.NoError(err)
	s.NotEmpty(result)
}

func (s *TypstCompilerSuite) TestTemplateCompilation() {
	// Create a simple template file
	templatePath := filepath.Join(s.templateDir, "simple_template.typ")
	err := os.WriteFile(templatePath, []byte(`
	#let data = json(sys.inputs.path)
	Hello, data.name!
	`), 0644)
	s.Require().NoError(err)

	// Compile the template with JSON input
	jsonInput := []byte(`{"name": "World"}`)
	_, err = s.compiler.CompileTemplate("simple_template.typ", jsonInput)

	// Ensure template compilation does not fail
	s.NoError(err)
}

// Test that the template compilation fails if the JSON input is missing a key
func (s *TypstCompilerSuite) TestTemplateCompilationEdgeCases() {
	// Create a simple Typst file
	testInputPath := filepath.Join(s.tempDir, "edge_case.typ")
	err := os.WriteFile(testInputPath, []byte(`
	#let data = json(sys.inputs.path)
	Hello, data.name!
	`), 0644)
	s.Require().NoError(err)

	// Compile with missing JSON key
	jsonInput := []byte(`{}`)
	_, err = s.compiler.CompileTemplate("edge_case.typ", jsonInput)

	// Ensure compilation fails due to missing key
	s.Error(err)
}

func (s *TypstCompilerSuite) TestInvoiceCompilation() {
	// Create a JSON input for the invoice
	invoiceJSON := []byte(`{
		"currency": "USD",
		"invoice_status": "paid",
		"invoice_number": "12345",
		"issuing_date": "2023-01-01",
		"due_date": "2023-01-10",
		"amount_due": 100.00,
		"notes": "Thank you for your business!",
		"vat": 5.00,
		"biller": {
			"name": "Company Inc.",
			"email": "contact@company.com",
			"help_email": "help@company.com",
			"address": {
				"street": "123 Business Rd.",
				"city": "Business City",
				"postal_code": "12345",
				"state": "CA",
				"country": "USA"
			}
		},
		"recipient": {
			"name": "John Doe",
			"email": "john.doe@example.com",
			"address": {
				"street": "456 Customer St.",
				"city": "Customer City",
				"postal_code": "67890",
				"state": "NY",
				"country": "USA"
			}
		},
		"line_items": [],
		"styling": {
			"font": "Inter",
			"secondary_color": "#919191"
		}
	}`)

	// Compile the invoice template
	_, err := s.compiler.CompileTemplate(types.TemplateInvoiceDefault, invoiceJSON)

	// Ensure invoice compilation does not fail
	s.NoError(err)
}

func (s *TypstCompilerSuite) TestOneTimeInvoiceCompilation() {
	// Create a JSON input for a one-time invoice (e.g., purchased credits)
	invoiceJSON := []byte(`{
		"currency": "USD",
		"invoice_status": "paid",
		"invoice_number": "CRED-123",
		"issuing_date": "2023-01-01",
		"due_date": "2023-01-10",
		"vat": 0.00,
		"amount_due": 25.00,
		"notes": "One-time credit purchase",
		"biller": {
			"name": "Company Inc.",
			"email": "contact@company.com",
			"help_email": "help@company.com",
			"address": {
				"street": "123 Business Rd.",
				"city": "Business City",
				"postal_code": "12345",
				"state": "CA",
				"country": "USA"
			}
		},
		"recipient": {
			"name": "John Doe",
			"email": "john.doe@example.com",
			"address": {
				"street": "456 Customer St.",
				"city": "Customer City",
				"postal_code": "67890",
				"state": "NY",
				"country": "USA"
			}
		},
		"line_items": [
			{
				"display_name": "Purchased Credits",
				"amount": 25.00,
				"quantity": 1,
				"currency": "USD",
				"period_start": "",
				"period_end": ""
			}
		],
		"styling": {
			"font": "Inter",
			"secondary_color": "#919191"
		}
	}`)

	// Compile the invoice template
	_, err := s.compiler.CompileTemplate(types.TemplateInvoiceDefault, invoiceJSON)

	// Ensure one-time invoice compilation does not fail
	s.NoError(err)
}
