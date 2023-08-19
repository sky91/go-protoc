package internal

import (
	"time"
)

// PackagePublic is a copy of go sdk: cmd/go/internal/load/pkg.go
type PackagePublic struct {
	// Note: These fields are part of the go command's public API.
	// See list.go. It is okay to add fields, but not to change or
	// remove existing ones. Keep in sync with list.go
	Dir           string        `json:",omitempty"` // directory containing package sources
	ImportPath    string        `json:",omitempty"` // import path of package in dir
	ImportComment string        `json:",omitempty"` // path in import comment on package statement
	Name          string        `json:",omitempty"` // package name
	Doc           string        `json:",omitempty"` // package documentation string
	Target        string        `json:",omitempty"` // installed target for this package (may be executable)
	Shlib         string        `json:",omitempty"` // the shared library that contains this package (only set when -linkshared)
	Root          string        `json:",omitempty"` // Go root, Go path dir, or module root dir containing this package
	ConflictDir   string        `json:",omitempty"` // Dir is hidden by this other directory
	ForTest       string        `json:",omitempty"` // package is only for use in named test
	Export        string        `json:",omitempty"` // file containing export data (set by go list -export)
	BuildID       string        `json:",omitempty"` // build ID of the compiled package (set by go list -export)
	Module        *ModulePublic `json:",omitempty"` // info about package's module, if any
	Match         []string      `json:",omitempty"` // command-line patterns matching this package
	Goroot        bool          `json:",omitempty"` // is this package found in the Go root?
	Standard      bool          `json:",omitempty"` // is this package part of the standard Go library?
	DepOnly       bool          `json:",omitempty"` // package is only as a dependency, not explicitly listed
	BinaryOnly    bool          `json:",omitempty"` // package cannot be recompiled
	Incomplete    bool          `json:",omitempty"` // was there an error loading this package or dependencies?

	// Stale and StaleReason remain here *only* for the list command.
	// They are only initialized in preparation for list execution.
	// The regular build determines staleness on the fly during action execution.
	Stale       bool   `json:",omitempty"` // would 'go install' do anything for this package?
	StaleReason string `json:",omitempty"` // why is Stale true?

	// Source files
	// If you add to this list you MUST add to p.AllFiles (below) too.
	// Otherwise file name security lists will not apply to any new additions.
	GoFiles           []string `json:",omitempty"` // .go source files (excluding CgoFiles, TestGoFiles, XTestGoFiles)
	CgoFiles          []string `json:",omitempty"` // .go source files that import "C"
	CompiledGoFiles   []string `json:",omitempty"` // .go output from running cgo on CgoFiles
	IgnoredGoFiles    []string `json:",omitempty"` // .go source files ignored due to build constraints
	IgnoredOtherFiles []string `json:",omitempty"` // non-.go source files ignored due to build constraints
	CFiles            []string `json:",omitempty"` // .c source files
	CXXFiles          []string `json:",omitempty"` // .cc, .cpp and .cxx source files
	MFiles            []string `json:",omitempty"` // .m source files
	HFiles            []string `json:",omitempty"` // .h, .hh, .hpp and .hxx source files
	FFiles            []string `json:",omitempty"` // .f, .F, .for and .f90 Fortran source files
	SFiles            []string `json:",omitempty"` // .s source files
	SwigFiles         []string `json:",omitempty"` // .swig files
	SwigCXXFiles      []string `json:",omitempty"` // .swigcxx files
	SysoFiles         []string `json:",omitempty"` // .syso system object files added to package

	// Embedded files
	EmbedPatterns []string `json:",omitempty"` // //go:embed patterns
	EmbedFiles    []string `json:",omitempty"` // files matched by EmbedPatterns

	// Cgo directives
	CgoCFLAGS    []string `json:",omitempty"` // cgo: flags for C compiler
	CgoCPPFLAGS  []string `json:",omitempty"` // cgo: flags for C preprocessor
	CgoCXXFLAGS  []string `json:",omitempty"` // cgo: flags for C++ compiler
	CgoFFLAGS    []string `json:",omitempty"` // cgo: flags for Fortran compiler
	CgoLDFLAGS   []string `json:",omitempty"` // cgo: flags for linker
	CgoPkgConfig []string `json:",omitempty"` // cgo: pkg-config names

	// Dependency information
	Imports   []string          `json:",omitempty"` // import paths used by this package
	ImportMap map[string]string `json:",omitempty"` // map from source import to ImportPath (identity entries omitted)
	Deps      []string          `json:",omitempty"` // all (recursively) imported dependencies

	// Error information
	// Incomplete is above, packed into the other bools
	Error      *PackageError   `json:",omitempty"` // error loading this package (not dependencies)
	DepsErrors []*PackageError `json:",omitempty"` // errors loading dependencies

	// Test information
	// If you add to this list you MUST add to p.AllFiles (below) too.
	// Otherwise file name security lists will not apply to any new additions.
	TestGoFiles        []string `json:",omitempty"` // _test.go files in package
	TestImports        []string `json:",omitempty"` // imports from TestGoFiles
	TestEmbedPatterns  []string `json:",omitempty"` // //go:embed patterns
	TestEmbedFiles     []string `json:",omitempty"` // files matched by TestEmbedPatterns
	XTestGoFiles       []string `json:",omitempty"` // _test.go files outside package
	XTestImports       []string `json:",omitempty"` // imports from XTestGoFiles
	XTestEmbedPatterns []string `json:",omitempty"` // //go:embed patterns
	XTestEmbedFiles    []string `json:",omitempty"` // files matched by XTestEmbedPatterns
}

// PackageError is a copy of go sdk: cmd/go/internal/load/pkg.go
// A PackageError describes an error loading information about a package.
type PackageError struct {
	ImportStack []string `json:",omitempty"`
	Pos         string   `json:",omitempty"`
	Err         string   `json:",omitempty"`
}

// ModulePublic is a copy of go sdk: cmd/go/internal/modinfo/info.go
type ModulePublic struct {
	Path      string        `json:",omitempty"` // module path
	Version   string        `json:",omitempty"` // module version
	Versions  []string      `json:",omitempty"` // available module versions
	Replace   *ModulePublic `json:",omitempty"` // replaced by this module
	Time      *time.Time    `json:",omitempty"` // time version was created
	Update    *ModulePublic `json:",omitempty"` // available update (with -u)
	Main      bool          `json:",omitempty"` // is this the main module?
	Indirect  bool          `json:",omitempty"` // module is only indirectly needed by main module
	Dir       string        `json:",omitempty"` // directory holding local copy of files, if any
	GoMod     string        `json:",omitempty"` // path to go.mod file describing module, if any
	GoVersion string        `json:",omitempty"` // go version used in module
	Retracted []string      `json:",omitempty"` // retraction information, if any (with -retracted or -u)
	Error     *ModuleError  `json:",omitempty"` // error loading module
}

// ModuleError is a copy of go sdk: cmd/go/internal/modinfo/info.go
type ModuleError struct {
	Err string // error text
}
