package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DirName string `json:"dir_name"`
	OutDir  string `json:"out_dir"`
}

var (
	ErrProcessFailure            = errors.New("critical process failure")
	ErrExtractFailure            = errors.New("critical extracting failure")
	ErrDirectoryFailure          = errors.New("direcotry reading error")
	ErrSchemaVerificationFailure = errors.New("function schema verification failed")
	ErrInvalidPackage            = errors.New("invalid package name")
	ErrInvalidMod                = errors.New("critical go.mod error")
	ErrInvalidRequestOutputs     = errors.New("critical output creation failure")
)

func main() {
	err := ProcessFunctions()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	log.Println("flowcli binary executed successfully!")
}

func ProcessFunctions() error {
	const configFile = "flowconfig.json"

	// Load configuration
	config, err := loadConfig(configFile)
	if err != nil {
		eInfo := fmt.Errorf("error loading config: %s", err)
		return errors.Join(ErrProcessFailure, eInfo, err)
	}

	// Validate input directory
	if err := validateDir(config.DirName); err != nil {
		eInfo := fmt.Errorf("configuration directory error: %s", err)
		return errors.Join(ErrDirectoryFailure, eInfo, err)
	}

	// Extract functions from the directory
	funcMap, err := extractFunctions(config.DirName)
	if err != nil {
		eInfo := fmt.Errorf("error extracting functions: %s", err)
		return errors.Join(ErrExtractFailure, eInfo, err)
	}

	// Extract package name
	packageName, err := getPackageName(config.DirName)
	if err != nil {
		eInfo := fmt.Errorf("error extracting package name: %s", err)
		return errors.Join(ErrInvalidPackage, eInfo, err)
	}

	// Get base URL from go.mod
	baseURL, err := getBaseURL()
	if err != nil {
		eInfo := fmt.Errorf("error extracting base URL: %s", err)
		return errors.Join(ErrInvalidMod, eInfo, err)
	}

	// Generate the output file
	if err := createOutputFile(config.OutDir, config.DirName, funcMap, baseURL+"/"+packageName); err != nil {
		eInfo := fmt.Errorf("error creating output file: %s", err)
		return errors.Join(ErrInvalidRequestOutputs, eInfo, err)
	}

	return nil
}

// loadConfig reads and parses the configuration from a JSON file.
func loadConfig(fileName string) (*Config, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("unable to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid config format: %w", err)
	}

	return &config, nil
}

// validateDir checks if a directory exists.
func validateDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' does not exist", dir)
	}
	return nil
}

// extractFunctions scans the directory and maps functions that match the required signature.
func extractFunctions(dir string) (map[string]string, error) {
	funcMap := make(map[string]string)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
			if err != nil {
				return err
			}

			for _, decl := range node.Decls {
				if funcDecl, ok := decl.(*ast.FuncDecl); ok {
					if isValidFuncType(funcDecl) {
						funcMap[funcDecl.Name.Name] = funcDecl.Name.Name
					}
				}
			}
		}
		return nil
	})

	return funcMap, err
}

func isValidFuncType(funcDecl *ast.FuncDecl) bool {
	// Check if the function has parameters
	if funcDecl.Type.Params == nil || len(funcDecl.Type.Params.List) != 2 {
		return false
	}

	// Validate first parameter: *ProcessContext
	firstParam := funcDecl.Type.Params.List[0]
	if !isType(firstParam.Type, "*flow.ProcessContext") {
		return false
	}

	// Validate second parameter: []DefinedInput
	secondParam := funcDecl.Type.Params.List[1]
	if !isType(secondParam.Type, "[]flow.DefinedInput") {
		return false
	}

	return true
}

// Helper function to check if the parameter matches the required type
func isType(expr ast.Expr, expectedType string) bool {
	switch t := expr.(type) {
	case *ast.StarExpr: // Handle pointer types
		if expectedType[0] == '*' {
			return isType(t.X, expectedType[1:])
		}
	case *ast.ArrayType: // Handle slice types
		if len(expectedType) > 2 && expectedType[:2] == "[]" {
			return isType(t.Elt, expectedType[2:])
		}
	case *ast.Ident: // Handle identifiers
		return t.Name == expectedType
	case *ast.SelectorExpr: // Handle package-prefixed types
		if sel, ok := t.X.(*ast.Ident); ok {
			return sel.Name+"."+t.Sel.Name == expectedType
		}
	}
	return false
}

// getPackageName retrieves the package name from the directory's Go files.
func getPackageName(dir string) (string, error) {
	var packageName string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
			if err != nil {
				return fmt.Errorf("error parsing file %s: %w", path, err)
			}
			packageName = node.Name.Name
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if packageName == "" {
		return "", fmt.Errorf("no Go files found in directory: %s", dir)
	}

	return packageName, nil
}

func getBaseURL() (string, error) {
	// Get the current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("error getting current directory: %w", err)
	}

	// Check for go.mod file in the same directory
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("go.mod file not found in the current directory: %s", dir)
		}
		return "", fmt.Errorf("error checking go.mod file: %w", err)
	}

	// Open the go.mod file
	file, err := os.Open(goModPath)
	if err != nil {
		return "", fmt.Errorf("error opening go.mod: %w", err)
	}
	defer file.Close()

	// Scan the file for the module name
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}

	// If no module name is found
	return "", fmt.Errorf("module name not found in go.mod")
}

// createOutputFile generates the out.go file in the specified output directory.
func createOutputFile(outDir, dirName string, funcMap map[string]string, packageName string) error {
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	outFilePath := filepath.Join(outDir, "out.go")
	file, err := os.Create(outFilePath)
	if err != nil {
		return fmt.Errorf("failed to create out.go: %w", err)
	}
	defer file.Close()

	builder := &strings.Builder{}
	builder.WriteString(fmt.Sprintf(`package output

import (
	"fmt"
	"github.com/e4coder/flow"
	"%s"
)

var FuncMap = make(map[string]flow.ProcessHandler)

func Init() {
`, packageName))

	for name := range funcMap {
		builder.WriteString(fmt.Sprintf("\tFuncMap[\"%s\"] = %s.%s\n", name, filepath.Base(dirName), name))
	}

	builder.WriteString(`}

func GetFuncByName(name string) (flow.ProcessHandler, error) {
	fn, ok := FuncMap[name]
	if !ok {
		return nil, fmt.Errorf("function %s not found", name)
	}
	return fn, nil
}
`)

	_, err = file.WriteString(builder.String())
	return err
}
